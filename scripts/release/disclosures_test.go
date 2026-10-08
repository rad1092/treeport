package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivesContainCommittedDisclosures(t *testing.T) {
	root := filepath.Join("..", "..")
	disclosure, err := loadDisclosures(root)
	if err != nil {
		t.Fatal(err)
	}
	// Removing a whole required notice and its manifest entry must not make the
	// distribution test pass merely because the remaining hashes still match.
	required := map[string]bool{
		"third_party/go/LICENSE": false, "third_party/go/PATENTS": false,
		"third_party/golang.org-x-text/LICENSE": false, "third_party/golang.org-x-text/PATENTS": false,
		manifestPath: false, disclosureReadme: false,
	}
	for _, file := range disclosure.Files {
		if len(file.data) == 0 {
			t.Fatalf("empty disclosure %s", file.name)
		}
		if _, ok := required[file.name]; ok {
			required[file.name] = true
		}
	}
	for name, present := range required {
		if !present {
			t.Fatalf("required notice omitted: %s", name)
		}
	}
	license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"tar", "zip"} {
		t.Run(format, func(t *testing.T) {
			name := "treeport"
			if format == "zip" {
				name += ".exe"
			}
			files := archiveFiles(name, []byte("binary"), license, readme, []byte("{}\n"), disclosure)
			var encoded bytes.Buffer
			write := writeTar
			if format == "zip" {
				write = writeZIP
			}
			if err := write(&encoded, files); err != nil {
				t.Fatal(err)
			}
			actual := unpack(t, encoded.Bytes(), format)
			if len(actual) != len(files) {
				t.Fatalf("archive member count %d; want %d", len(actual), len(files))
			}
			for _, file := range files {
				if !bytes.Equal(actual[file.name], file.data) {
					t.Fatalf("archive omitted or changed %s", file.name)
				}
			}
		})
	}
}

func unpack(t *testing.T, data []byte, format string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	add := func(name string, reader io.Reader) {
		if _, exists := files[name]; exists {
			t.Fatalf("duplicate archive member %s", name)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = body
	}
	if format == "zip" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range archive.File {
			reader, err := member.Open()
			if err != nil {
				t.Fatal(err)
			}
			add(member.Name, reader)
			reader.Close()
		}
		return files
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	for {
		member, err := archive.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		add(member.Name, archive)
	}
}

func copyDisclosureFixture(t *testing.T) (string, disclosureManifest) {
	t.Helper()
	root := t.TempDir()
	disclosure, err := loadDisclosures(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range disclosure.Files {
		full := filepath.Join(root, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, file.data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root, disclosure.Manifest
}

func TestDisclosureOmissionTamperAndUnsafePaths(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, root string, manifest *disclosureManifest)
	}{
		{"missing file", func(t *testing.T, root string, m *disclosureManifest) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(m.Files[0].Path))); err != nil {
				t.Fatal(err)
			}
		}},
		{"tampered file", func(t *testing.T, root string, m *disclosureManifest) {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(m.Files[0].Path)), []byte("tampered"), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"omitted entry", func(t *testing.T, _ string, m *disclosureManifest) { m.Files = m.Files[1:] }},
		{"omitted entry and file", func(t *testing.T, root string, m *disclosureManifest) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(m.Files[0].Path))); err != nil {
				t.Fatal(err)
			}
			m.Files = m.Files[1:]
		}},
		{"duplicate path", func(t *testing.T, _ string, m *disclosureManifest) { m.Files = append(m.Files, m.Files[0]) }},
		{"manifest collision", func(t *testing.T, _ string, m *disclosureManifest) { m.Files[0].Path = manifestPath }},
		{"parent traversal", func(t *testing.T, _ string, m *disclosureManifest) { m.Files[0].Path = "third_party/../../secret" }},
		{"absolute path", func(t *testing.T, _ string, m *disclosureManifest) { m.Files[0].Path = "/third_party/LICENSE" }},
		{"backslash", func(t *testing.T, _ string, m *disclosureManifest) { m.Files[0].Path = `third_party/..\secret` }},
		{"bad hash", func(t *testing.T, _ string, m *disclosureManifest) { m.Files[0].SHA256 = "123" }},
		{"wrong schema", func(t *testing.T, _ string, m *disclosureManifest) { m.SchemaVersion = 2 }},
		{"missing modules", func(t *testing.T, _ string, m *disclosureManifest) { m.Modules = nil }},
		{"missing Go version", func(t *testing.T, _ string, m *disclosureManifest) { m.GoVersion = "" }},
		{"missing files", func(t *testing.T, _ string, m *disclosureManifest) { m.Files = nil }},
		{"symlink", func(t *testing.T, root string, m *disclosureManifest) {
			name := filepath.Join(root, filepath.FromSlash(m.Files[0].Path))
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "third_party", "README.md"), name); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifest := copyDisclosureFixture(t)
			test.edit(t, root, &manifest)
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(manifestPath)), raw, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDisclosures(root); err == nil {
				t.Fatal("invalid disclosures accepted")
			}
		})
	}
}

func TestDisclosureRejectsAdditionalJSONAndUnknownFields(t *testing.T) {
	for _, suffix := range []string{"{}", ",\"unrecognized\":true}"} {
		root, manifest := copyDisclosureFixture(t)
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(suffix, ",") {
			raw = raw[:len(raw)-1]
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(manifestPath)), append(raw, suffix...), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDisclosures(root); err == nil {
			t.Fatal("invalid disclosure JSON accepted")
		}
	}
}
