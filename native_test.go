package treeport_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rad1092/treeport"
	"github.com/rad1092/treeport/input"
)

// TestNativeFilesystem probes only a disposable directory; its log is evidence
// for that volume, never an inference that all mounts on an OS behave identically.
func TestNativeFilesystem(t *testing.T) {
	root := t.TempDir()
	if parent := os.Getenv("TREEPORT_NATIVE_ROOT"); parent != "" {
		var err error
		root, err = os.MkdirTemp(parent, "treeport-native-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	create := func(name string) error {
		f, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		return f.Close()
	}
	if err := create("CaseProbe"); err != nil {
		t.Fatal(err)
	}
	err := create("caseprobe")
	caseSensitive := err == nil
	if err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	if err := create("café"); err != nil {
		t.Fatal(err)
	}
	err = create("cafe\u0301")
	normalizationSensitive := err == nil
	if err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	if err := create("한국어😀"); err != nil {
		t.Fatal(err)
	}
	t.Logf("NATIVE_FILESYSTEM os=%s arch=%s case_sensitive=%t normalization_sensitive=%t", runtime.GOOS, runtime.GOARCH, caseSensitive, normalizationSensitive)
	entries, err := input.Tree(context.Background(), root, treeport.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	expected := 3
	if caseSensitive {
		expected++
	}
	if normalizationSensitive {
		expected++
	}
	if len(entries) != expected {
		t.Fatalf("enumerated %d; expected %d", len(entries), expected)
	}
	// Keep the actual observed names. Candidate analysis must preserve originals.
	r, err := treeport.Check(context.Background(), entries, treeport.Options{Profile: "export-fold"})
	if err != nil {
		t.Fatal(err)
	}
	groups := 0
	if caseSensitive {
		groups++
	}
	if normalizationSensitive {
		groups++
	}
	if len(r.Conflicts) != groups {
		t.Fatalf("expected %d candidate groups, got %+v", groups, r.Conflicts)
	}
	if runtime.GOOS == "windows" {
		err := create("COM¹")
		accepted := err == nil
		t.Logf("NATIVE_WINDOWS go_open_reserved_superscript_accepted=%t", accepted)
		if accepted {
			if err := os.Remove(filepath.Join(root, "COM¹")); err != nil {
				t.Fatal(err)
			}
		}
		// Modern APIs may accept a spelling forbidden by Microsoft's documented
		// shell portability policy. Observe that distinction rather than assuming
		// that policy rejection is an exact syscall/filesystem emulator.
		policy, err := treeport.Check(context.Background(), []treeport.Entry{{Path: "COM¹", Kind: "file"}}, treeport.Options{Profile: "windows", DestinationRoot: `C:\dst`})
		if err != nil || policy.Status != "incompatible" {
			t.Fatalf("documented reserved-name policy not enforced: %+v %v", policy, err)
		}
	}
	// Re-enumeration proves that analysis did not rename or change the source.
	after, err := input.Tree(context.Background(), root, treeport.DefaultLimits())
	if err != nil || len(after) != len(entries) {
		t.Fatalf("source enumeration changed: %v", err)
	}
}

// Verify the boundary against a real path: on Unix hosts a backslash is part
// of the directory name, so it must consume a byte in the destination budget.
func TestNativeFilesystemLiteralBackslashRootBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a Windows separator; literal-backslash fixture requires a Unix host")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, `a\`)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "x")
	content := []byte("literal-backslash-root\n")
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(parent)
	if err != nil || len(items) != 1 || items[0].Name() != `a\` {
		t.Fatalf("literal backslash not preserved: %v %v", items, err)
	}
	entries, err := input.Tree(context.Background(), root, treeport.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"posix", "macos"} {
		for _, trailingSlash := range []bool{false, true} {
			destination := root
			if trailingSlash {
				destination += "/"
			}
			for _, delta := range []int{-1, 0} {
				limit := len(file) + delta
				r, err := treeport.Check(context.Background(), entries, treeport.Options{Profile: profile, DestinationRoot: destination, MaxPath: limit})
				if err != nil {
					t.Fatal(err)
				}
				want := "known-compatible"
				if delta < 0 {
					want = "incompatible"
				}
				if r.Status != want {
					t.Errorf("profile=%s trailing_slash=%t actual_bytes=%d limit=%d status=%s want=%s", profile, trailingSlash, len(file), limit, r.Status, want)
				}
			}
		}
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(content) {
		t.Fatal("analysis changed native fixture content", err)
	}
	t.Logf("NATIVE_ROOT_BUDGET os=%s literal_backslash=true actual_path_bytes=%d source_unchanged=true", runtime.GOOS, len(file))
}
