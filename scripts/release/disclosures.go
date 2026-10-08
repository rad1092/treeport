package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const manifestPath = "third_party/manifest.json"
const disclosureReadme = "third_party/README.md"

var requiredNotices = []string{
	"third_party/go/LICENSE", "third_party/go/PATENTS",
	"third_party/golang.org-x-text/LICENSE", "third_party/golang.org-x-text/PATENTS",
}

type disclosureFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
}

type disclosureManifest struct {
	SchemaVersion int               `json:"schema_version"`
	GoVersion     string            `json:"go_version"`
	Modules       map[string]string `json:"modules"`
	Files         []disclosureFile  `json:"files"`
}

type disclosures struct {
	Manifest disclosureManifest
	Hash     string
	Files    []payload
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeDisclosurePath(name string) bool {
	return strings.HasPrefix(name, "third_party/") && fs.ValidPath(name) && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00\r\n")
}

func loadDisclosures(root string) (disclosures, error) {
	var result disclosures
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestPath)))
	if err != nil {
		return result, fmt.Errorf("disclosure manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Manifest); err != nil {
		return result, fmt.Errorf("disclosure manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("disclosure manifest must contain exactly one JSON object")
	}
	m := result.Manifest
	if m.SchemaVersion != 1 || !strings.HasPrefix(m.GoVersion, "go1.") || len(m.Modules) == 0 || len(m.Files) == 0 {
		return result, fmt.Errorf("disclosure manifest requires schema 1, a Go version, modules and files")
	}
	for name, version := range m.Modules {
		if name == "" || name == mainModulePath || !strings.HasPrefix(version, "v") {
			return result, fmt.Errorf("invalid disclosed module %q at %q", name, version)
		}
	}
	expected := map[string]string{manifestPath: digest(raw), disclosureReadme: ""}
	for _, file := range m.Files {
		if !safeDisclosurePath(file.Path) {
			return result, fmt.Errorf("unsafe disclosure path %q", file.Path)
		}
		if _, exists := expected[file.Path]; exists {
			return result, fmt.Errorf("duplicate disclosure path %q", file.Path)
		}
		hash, err := hex.DecodeString(file.SHA256)
		if err != nil || len(hash) != sha256.Size || strings.ToLower(file.SHA256) != file.SHA256 {
			return result, fmt.Errorf("invalid SHA-256 for %q", file.Path)
		}
		source, err := url.Parse(file.Source)
		if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
			return result, fmt.Errorf("disclosure source must be an HTTPS URL for %q", file.Path)
		}
		expected[file.Path] = file.SHA256
	}
	for _, name := range requiredNotices {
		if _, ok := expected[name]; !ok {
			return result, fmt.Errorf("required disclosure omitted: %s", name)
		}
	}
	// The inventory catches a notice accidentally omitted from the manifest.
	// This validates the checked-in disclosure set, not legal sufficiency.
	err = filepath.WalkDir(filepath.Join(root, "third_party"), func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("disclosure symlink is unsupported: %s", full)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("disclosure is not a regular file: %s", full)
		}
		relative, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		want, ok := expected[name]
		if !ok {
			return fmt.Errorf("disclosure file is absent from manifest: %s", name)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		if want != "" && digest(data) != want {
			return fmt.Errorf("disclosure SHA-256 mismatch: %s", name)
		}
		result.Files = append(result.Files, payload{name, data, 0644})
		delete(expected, name)
		return nil
	})
	if err != nil {
		return result, err
	}
	if len(expected) != 0 {
		missing := make([]string, 0, len(expected))
		for name := range expected {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return result, fmt.Errorf("missing disclosure files: %s", strings.Join(missing, ", "))
	}
	result.Hash = digest(raw)
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].name < result.Files[j].name })
	return result, nil
}
