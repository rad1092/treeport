// Command release builds reproducible-layout release archives and SHA256SUMS.
// It writes only to the requested output directory and a temporary build tree.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/rad1092/treeport"
)

func main() {
	out := flag.String("out", "dist", "output directory (must not contain release files)")
	targets := flag.String("targets", "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64,windows/amd64,windows/arm64", "comma-separated GOOS/GOARCH targets")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := build(ctx, *out, *targets); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type payload struct {
	name string
	data []byte
	mode int64
}

func build(ctx context.Context, out, targetList string) error {
	disclosure, err := loadDisclosures(".")
	if err != nil {
		return err
	}
	if runtime.Version() != disclosure.Manifest.GoVersion {
		return fmt.Errorf("release toolchain %s differs from disclosed %s", runtime.Version(), disclosure.Manifest.GoVersion)
	}
	goCmd, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	// Ignore ambient workspaces, alternate module files and automatic toolchain
	// switching. Every release is checked against this checkout and manifest.
	goEnvironment := append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local")
	tmp, err := os.MkdirTemp("", "treeport-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	commitBytes, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	commit := strings.TrimSpace(string(commitBytes))
	builder, ok := debug.ReadBuildInfo()
	if !ok || builder.Main.Path != mainModulePath || builder.Main.Version == "" {
		return fmt.Errorf("release builder has no main module identity")
	}
	builderSettings := make(map[string]string)
	for _, setting := range builder.Settings {
		builderSettings[setting.Key] = setting.Value
	}
	if builderSettings["vcs.revision"] != commit || builderSettings["vcs.modified"] != "false" {
		return fmt.Errorf("release builder must be built from this exact clean Git commit; use go run -buildvcs=true ./scripts/release")
	}
	toolchain := exec.CommandContext(ctx, goCmd, "env", "GOVERSION")
	toolchain.Env = goEnvironment
	goVersion, err := toolchain.Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(goVersion)) != disclosure.Manifest.GoVersion {
		return fmt.Errorf("go executable differs from disclosed toolchain %s", disclosure.Manifest.GoVersion)
	}
	moduleCommand := exec.CommandContext(ctx, goCmd, "mod", "edit", "-json")
	moduleCommand.Env = goEnvironment
	moduleJSON, err := moduleCommand.Output()
	if err != nil {
		return fmt.Errorf("read module identity: %w", err)
	}
	if err := verifyModuleFile(bytes.NewReader(moduleJSON), disclosure.Manifest.Modules); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	targets := strings.Split(targetList, ",")
	sort.Strings(targets)
	seen := map[string]bool{}
	var sums bytes.Buffer
	for _, target := range targets {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || !supported(parts[0], parts[1]) || seen[target] {
			return fmt.Errorf("invalid or duplicate target %q", target)
		}
		seen[target] = true
		goos, arch := parts[0], parts[1]
		name := "treeport"
		if goos == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(tmp, name)
		cmd := exec.CommandContext(ctx, goCmd, "build", "-mod=readonly", "-buildvcs=true", "-trimpath", "-ldflags=-s -w -buildid=", "-o", binary, "./cmd/treeport")
		cmd.Env = append(goEnvironment, "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+arch)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s: %w", target, err)
		}
		identity := binaryIdentity{disclosure.Manifest.GoVersion, builder.Main.Version, commit, goos, arch, disclosure.Manifest.Modules}
		info, err := verifyBinary(binary, identity)
		if err != nil {
			return fmt.Errorf("verify %s: %w", target, err)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		meta, err := json.MarshalIndent(map[string]string{
			"version": treeport.Version, "commit": commit,
			"go_version": info.GoVersion, "target": target,
			"main_module_version": info.Main.Version, "disclosure_manifest_sha256": disclosure.Hash,
		}, "", "  ")
		if err != nil {
			return err
		}
		files := archiveFiles(name, data, license, readme, append(meta, '\n'), disclosure)
		var archive bytes.Buffer
		ext := ".tar.gz"
		if goos == "windows" {
			ext = ".zip"
			err = writeZIP(&archive, files)
		} else {
			err = writeTar(&archive, files)
		}
		if err != nil {
			return err
		}
		filename := "treeport_" + treeport.Version + "_" + goos + "_" + arch + ext
		if err := writeNew(filepath.Join(out, filename), archive.Bytes()); err != nil {
			return err
		}
		digest := sha256.Sum256(archive.Bytes())
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(digest[:]), filename)
		fmt.Println(filename)
	}
	return writeNew(filepath.Join(out, "SHA256SUMS"), sums.Bytes())
}

func archiveFiles(binaryName string, binary, license, readme, metadata []byte, disclosure disclosures) []payload {
	files := []payload{{"LICENSE", license, 0644}, {"README.md", readme, 0644}, {"build.json", metadata, 0644}, {binaryName, binary, 0755}}
	return append(files, disclosure.Files...)
}

func supported(goos, arch string) bool {
	return (goos == "darwin" || goos == "linux" || goos == "windows") && (arch == "amd64" || arch == "arm64")
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func writeTar(out io.Writer, files []payload) error {
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.data)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		if _, err := tw.Write(file.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZIP(out io.Writer, files []payload) error {
	zw := zip.NewWriter(out)
	for _, file := range files {
		h := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		h.SetMode(os.FileMode(file.mode))
		h.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(file.data); err != nil {
			return err
		}
	}
	return zw.Close()
}
