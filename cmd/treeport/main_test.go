package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rad1092/treeport"
)

func TestCLIArchiveDoesNotExtract(t *testing.T) {
	dir := t.TempDir()
	var raw bytes.Buffer
	archive := zip.NewWriter(&raw)
	w, err := archive.Create("../would-escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("do not extract")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "source.zip")
	if err := os.WriteFile(path, raw.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run(context.Background(), []string{"zip", "--profile", "posix", "--json", path}, nil, &out, io.Discard); code != 1 {
		t.Fatalf("ZIP traversal: exit=%d %s", code, out.String())
	}
	if !strings.Contains(out.String(), "unsafe_path") {
		t.Fatalf("missing traversal explanation: %s", out.String())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "source.zip" {
		t.Fatalf("unexpected source directory changes: %v %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "would-escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive entry escaped the source directory: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw.Bytes()) {
		t.Fatal("source ZIP changed")
	}
}

func TestCLIStatusAndErrorContract(t *testing.T) {
	tests := []struct {
		name, input, status string
		args                []string
		code                int
	}{
		{"compatible", `{"path":"src/main.go"}`, "known-compatible", []string{"manifest", "--json", "--profile", "posix", "-"}, 0},
		{"incompatible", "{\"path\":\"Readme\"}\n{\"path\":\"README\"}", "incompatible", []string{"manifest", "--json", "--profile", "windows", "--root", `C:\dest`, "-"}, 1},
		{"unknown", `{"path":"한글.txt"}`, "unknown", []string{"manifest", "--json", "--profile", "windows", "--root", `C:\dest`, "-"}, 3},
		{"missing profile", "", "error", []string{"manifest", "--json", "-"}, 2},
		{"missing Windows root", "", "error", []string{"manifest", "--profile", "windows", "--json", "-"}, 2},
		{"malformed manifest", "{", "error", []string{"manifest", "--json", "--profile", "posix", "-"}, 2},
		{"unknown flag", "", "error", []string{"manifest", "--wrong", "--json", "-"}, 2},
		{"flag after input", "", "error", []string{"manifest", "--profile", "posix", "-", "--json"}, 2},
		{"zero limit", "", "error", []string{"manifest", "--json", "--profile", "posix", "--max-entries", "0", "-"}, 2},
		{"zero timeout", "", "error", []string{"manifest", "--json", "--profile", "posix", "--timeout", "0", "-"}, 2},
		{"entry budget", "{\"path\":\"a\"}\n{\"path\":\"b\"}", "error", []string{"manifest", "--json", "--profile", "posix", "--max-entries", "1", "-"}, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), test.args, strings.NewReader(test.input), &stdout, &stderr)
			if code != test.code {
				t.Fatalf("exit %d, want %d: stdout=%s stderr=%s", code, test.code, stdout.String(), stderr.String())
			}
			var body struct {
				Status, Error string
				SchemaVersion string `json:"schema_version"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
				t.Fatalf("single JSON response required: %v: %s", err, stdout.String())
			}
			if body.Status != test.status || body.SchemaVersion != treeport.SchemaVersion {
				t.Fatalf("response: %+v", body)
			}
			if test.code == 2 && body.Error == "" {
				t.Fatal("error response has no explanation")
			}
			if stderr.Len() != 0 {
				t.Fatalf("JSON mode wrote stderr: %s", stderr.String())
			}
		})
	}
}

func TestCLIEnumerationOrderDoesNotChangeJSON(t *testing.T) {
	inputs := []string{
		"{\"path\":\"Readme/a.txt\"}\n{\"path\":\"README/A.TXT\"}",
		"{\"path\":\"README/A.TXT\"}\n{\"path\":\"Readme/a.txt\"}",
	}
	var outputs [2]bytes.Buffer
	for i, input := range inputs {
		if code := run(context.Background(), []string{"manifest", "--profile", "windows", "--root", `C:\dest`, "--json", "-"}, strings.NewReader(input), &outputs[i], io.Discard); code != 1 {
			t.Fatalf("exit %d: %s", code, outputs[i].String())
		}
	}
	if !bytes.Equal(outputs[0].Bytes(), outputs[1].Bytes()) {
		t.Fatal("enumeration order changed the JSON report")
	}
}

func TestCLIPipeDeadline(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	var out bytes.Buffer
	start := time.Now()
	code := run(context.Background(), []string{"manifest", "--profile", "posix", "--json", "--timeout", "20ms", "-"}, r, &out, io.Discard)
	if code != 2 || !strings.Contains(out.String(), "deadline exceeded") {
		t.Fatalf("deadline result: exit=%d %s", code, out.String())
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("blocked input was not interrupted by the deadline")
	}
}

func TestHumanOutputEscapesFilenames(t *testing.T) {
	var out bytes.Buffer
	err := writeHuman(&out, treeport.Report{Issues: []treeport.Issue{{Path: treeport.Original{Display: "bad\n\x1b[31m"}}}, Conflicts: []treeport.Conflict{{Target: "bad\r\n", Members: []treeport.Member{{Path: treeport.Original{Display: "member\x1b"}}}}}})
	if err != nil || strings.ContainsRune(out.String(), '\x1b') || !strings.Contains(out.String(), `bad\n\x1b[31m`) {
		t.Fatalf("unsafe terminal output: %q (%v)", out.String(), err)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }

func TestOutputFailureReturnsError(t *testing.T) {
	for _, jsonFlag := range []bool{false, true} {
		args := []string{"manifest", "--profile", "posix"}
		if jsonFlag {
			args = append(args, "--json")
		}
		args = append(args, "-")
		var stderr bytes.Buffer
		if code := run(context.Background(), args, strings.NewReader(`{"path":"ok"}`), failedWriter{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "broken output") {
			t.Fatalf("write failure: exit=%d stderr=%s", code, stderr.String())
		}
	}
}

func TestInstalledBinarySmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("go install subprocess omitted in short mode")
	}
	binDir := t.TempDir()
	goExe := filepath.Join(runtime.GOROOT(), "bin", "go")
	name := "treeport"
	if runtime.GOOS == "windows" {
		goExe += ".exe"
		name += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	install := exec.CommandContext(ctx, goExe, "install", ".")
	install.Env = append(os.Environ(), "GOBIN="+binDir)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("go install: %v\n%s", err, output)
	}
	binary := filepath.Join(binDir, name)
	version, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil || string(version) != "treeport "+treeport.Version+"\n" {
		t.Fatalf("installed version: %v %s", err, version)
	}
	tree := t.TempDir()
	path := filepath.Join(tree, "README.txt")
	content := []byte("source must remain unchanged\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "scan", "--profile", "windows", "--root", `C:\dest`, "--json", tree)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installed scan: %v\n%s", err, output)
	}
	var report treeport.Report
	if err := json.Unmarshal(output, &report); err != nil || report.Status != "known-compatible" || report.EntryCount != 1 {
		t.Fatalf("installed scan report: %v %s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatal("scan changed source contents")
	}
	// Test the real OS stdin descriptor, including cancellation of a stalled pipe.
	pipeContext, stopPipe := context.WithTimeout(ctx, 5*time.Second)
	defer stopPipe()
	command = exec.CommandContext(pipeContext, binary, "manifest", "--profile", "posix", "--timeout", "50ms", "--json", "-")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var pipeOut bytes.Buffer
	command.Stdout, command.Stderr = &pipeOut, &pipeOut
	if err := command.Run(); err == nil {
		t.Fatal("stalled stdin unexpectedly succeeded")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("stalled stdin: %v: %s", err, pipeOut.String())
	}
	if !strings.Contains(pipeOut.String(), "deadline exceeded") {
		t.Fatalf("stalled stdin omitted deadline: %s", pipeOut.String())
	}
}
