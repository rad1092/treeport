package main

import (
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
)

const mainModulePath = "github.com/rad1092/treeport"

type listedModule struct {
	Path, Version string
}

// Verify the declared module file as well as the binary's linked dependency
// set. Dependency modules may themselves list build tools that are not shipped.
func verifyModuleFile(reader io.Reader, expected map[string]string) error {
	decoder := json.NewDecoder(reader)
	var file struct {
		Module  *listedModule
		Require []listedModule
		Replace []json.RawMessage
	}
	if err := decoder.Decode(&file); err != nil {
		return fmt.Errorf("module metadata: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("module metadata contains trailing data")
	}
	if file.Module == nil || file.Module.Path != mainModulePath {
		return fmt.Errorf("unexpected main module")
	}
	if len(file.Replace) != 0 {
		return fmt.Errorf("module replacements are not disclosed")
	}
	actual := make(map[string]string)
	for _, module := range file.Require {
		if _, exists := actual[module.Path]; exists {
			return fmt.Errorf("duplicate module %q", module.Path)
		}
		actual[module.Path] = module.Version
	}
	return compareModules(actual, expected)
}

func compareModules(actual, expected map[string]string) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("module set differs from disclosure manifest: got %d, want %d", len(actual), len(expected))
	}
	for name, want := range expected {
		if actual[name] != want {
			return fmt.Errorf("module %s differs from disclosure manifest: got %q, want %q", name, actual[name], want)
		}
	}
	return nil
}

type binaryIdentity struct {
	GoVersion, MainVersion, Commit, GOOS, GOARCH string
	Modules                                      map[string]string
}

func verifyBinary(filename string, want binaryIdentity) (*debug.BuildInfo, error) {
	info, err := buildinfo.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read binary build identity: %w", err)
	}
	if err := verifyBuildInfo(info, want); err != nil {
		return nil, err
	}
	return info, nil
}

func verifyBuildInfo(info *debug.BuildInfo, want binaryIdentity) error {
	if info.GoVersion != want.GoVersion || info.Main.Path != mainModulePath || info.Main.Version != want.MainVersion || info.Main.Replace != nil || info.Path != mainModulePath+"/cmd/treeport" {
		return fmt.Errorf("binary Go version, main module/version or command identity mismatch")
	}
	actual := make(map[string]string)
	for _, module := range info.Deps {
		if module.Replace != nil {
			return fmt.Errorf("binary includes module replacement: %s", module.Path)
		}
		if _, exists := actual[module.Path]; exists {
			return fmt.Errorf("binary includes duplicate module: %s", module.Path)
		}
		actual[module.Path] = module.Version
	}
	if err := compareModules(actual, want.Modules); err != nil {
		return err
	}
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		if _, exists := settings[setting.Key]; exists {
			return fmt.Errorf("binary includes duplicate setting: %s", setting.Key)
		}
		settings[setting.Key] = setting.Value
	}
	for key, value := range map[string]string{"vcs": "git", "vcs.revision": want.Commit, "vcs.modified": "false", "GOOS": want.GOOS, "GOARCH": want.GOARCH, "CGO_ENABLED": "0"} {
		if settings[key] != value {
			return fmt.Errorf("binary %s mismatch: got %q, want %q", key, settings[key], value)
		}
	}
	return nil
}
