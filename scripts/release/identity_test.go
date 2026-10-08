package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestModuleIdentityRejectsDriftAndReplacement(t *testing.T) {
	for _, test := range []struct{ name, required, replacement string }{
		{"valid", `[{"Path":"golang.org/x/text","Version":"v0.28.0"}]`, "[]"},
		{"version drift", `[{"Path":"golang.org/x/text","Version":"v0.29.0"}]`, "[]"},
		{"omitted module", `[]`, "[]"},
		{"extra module", `[{"Path":"golang.org/x/text","Version":"v0.28.0"},{"Path":"example.com/extra","Version":"v1.0.0"}]`, "[]"},
		{"duplicate module", `[{"Path":"golang.org/x/text","Version":"v0.28.0"},{"Path":"golang.org/x/text","Version":"v0.28.0"}]`, "[]"},
		{"replacement", `[{"Path":"golang.org/x/text","Version":"v0.28.0"}]`, `[{"Old":{"Path":"golang.org/x/text"},"New":{"Path":"../local"}}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"Module":{"Path":"` + mainModulePath + `"},"Require":` + test.required + `,"Replace":` + test.replacement + `}`
			err := verifyModuleFile(strings.NewReader(raw), map[string]string{"golang.org/x/text": "v0.28.0"})
			if (err == nil) != (test.name == "valid") {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}

func TestBinaryIdentityRejectsDriftAndMissingEvidence(t *testing.T) {
	want := binaryIdentity{"go1.27.1", "v0.1.3", strings.Repeat("a", 40), "linux", "amd64", map[string]string{"golang.org/x/text": "v0.28.0"}}
	valid := debug.BuildInfo{
		GoVersion: want.GoVersion, Path: mainModulePath + "/cmd/treeport",
		Main:     debug.Module{Path: mainModulePath, Version: want.MainVersion},
		Deps:     []*debug.Module{{Path: "golang.org/x/text", Version: "v0.28.0"}},
		Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: want.Commit}, {Key: "vcs.modified", Value: "false"}, {Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"}},
	}
	if err := verifyBuildInfo(&valid, want); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*debug.BuildInfo)
	}{
		{"toolchain", func(i *debug.BuildInfo) { i.GoVersion = "go1.25.0" }},
		{"main module", func(i *debug.BuildInfo) { i.Main.Path = "example.com/other" }},
		{"main version", func(i *debug.BuildInfo) { i.Main.Version = "v0.1.2" }},
		{"command", func(i *debug.BuildInfo) { i.Path += "-other" }},
		{"dependency version", func(i *debug.BuildInfo) { i.Deps[0].Version = "v0.29.0" }},
		{"dependency omitted", func(i *debug.BuildInfo) { i.Deps = nil }},
		{"extra dependency", func(i *debug.BuildInfo) {
			i.Deps = append(i.Deps, &debug.Module{Path: "example.com/extra", Version: "v1.0.0"})
		}},
		{"replacement", func(i *debug.BuildInfo) { i.Deps[0].Replace = &debug.Module{Path: "../local"} }},
		{"main replacement", func(i *debug.BuildInfo) { i.Main.Replace = &debug.Module{Path: "../local"} }},
		{"wrong commit", func(i *debug.BuildInfo) { i.Settings[1].Value = strings.Repeat("b", 40) }},
		{"dirty source", func(i *debug.BuildInfo) { i.Settings[2].Value = "true" }},
		{"wrong OS", func(i *debug.BuildInfo) { i.Settings[3].Value = "darwin" }},
		{"wrong architecture", func(i *debug.BuildInfo) { i.Settings[4].Value = "arm64" }},
		{"CGO enabled", func(i *debug.BuildInfo) { i.Settings[5].Value = "1" }},
		{"missing VCS metadata", func(i *debug.BuildInfo) { i.Settings = i.Settings[3:] }},
		{"duplicate settings", func(i *debug.BuildInfo) { i.Settings = append(i.Settings, i.Settings[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(valid)
			if err != nil {
				t.Fatal(err)
			}
			var info debug.BuildInfo
			if err := json.Unmarshal(raw, &info); err != nil {
				t.Fatal(err)
			}
			test.edit(&info)
			if err := verifyBuildInfo(&info, want); err == nil {
				t.Fatal("incorrect binary identity accepted")
			}
		})
	}
	file := filepath.Join(t.TempDir(), "not-an-executable")
	if err := os.WriteFile(file, []byte("corrupt executable"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBinary(file, want); err == nil {
		t.Fatal("binary without build metadata accepted")
	}
}
