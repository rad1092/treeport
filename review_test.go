package treeport_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rad1092/treeport"
)

// Different source spellings that only match under an uncertain Unicode
// comparison must not become a definite collision merely because kinds differ.
func TestReviewUncertainUnicodeTypeAlias(t *testing.T) {
	for _, tc := range []struct {
		name, profile, root, file, directory string
	}{
		{"macos_normalization", "macos", "/tmp", "é", "e\u0301"},
		{"windows_fold", "windows", `C:\dst`, "ß", "ss"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := treeport.Check(context.Background(), []treeport.Entry{
				{Path: tc.file, Kind: "file"},
				{Path: tc.directory, Kind: "directory"},
			}, treeport.Options{Profile: tc.profile, DestinationRoot: tc.root})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "unknown" || len(r.Conflicts) != 1 || r.Conflicts[0].Status != "unknown" {
				t.Fatalf("uncertain different-spelling alias was certified: %+v", r)
			}
		})
	}
}

// Filesystem Unicode tables are irrelevant when two entries use identical bytes.
func TestReviewIdenticalUnicodeFileSymlinkCollision(t *testing.T) {
	r, err := treeport.Check(context.Background(), []treeport.Entry{
		{Path: "한글", Kind: "file"},
		{Path: "한글", Kind: "symlink"},
	}, treeport.Options{Profile: "macos", DestinationRoot: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "incompatible" || len(r.Conflicts) != 1 || r.Conflicts[0].Status != "incompatible" {
		t.Fatalf("identical bytes cannot occupy both file and symlink slots: %+v", r)
	}
	if len(r.Conflicts[0].Causes) == 0 {
		t.Fatal("definite collision lacks an explanation")
	}
}

func TestReviewSlashRootCountsTowardPathBudget(t *testing.T) {
	r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: "a", Kind: "file"}}, treeport.Options{
		Profile: "posix", DestinationRoot: "/", MaxPath: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range r.Issues {
		found = found || issue.Code == "path_budget"
	}
	if r.Status != "incompatible" || !found {
		t.Fatalf("absolute /a takes two bytes, got %+v", r)
	}
}

func TestReviewCanceledEmptyCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := treeport.Check(ctx, nil, treeport.Options{Profile: "posix"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("already-canceled context returned %v", err)
	}
}

func TestReviewDefiniteSubsetSurvivesUncertainAlias(t *testing.T) {
	for _, tc := range []struct {
		name, profile, root string
		entries             []treeport.Entry
	}{
		{"duplicate", "macos", "/tmp", []treeport.Entry{{Path: "é"}, {Path: "é"}, {Path: "e\u0301"}}},
		{"type_collision", "macos", "/tmp", []treeport.Entry{{Path: "é", Kind: "file"}, {Path: "é", Kind: "directory"}, {Path: "e\u0301", Kind: "file"}}},
		{"ascii_pair", "windows", `C:\dst`, []treeport.Entry{{Path: "K"}, {Path: "k"}, {Path: "K"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := treeport.Check(context.Background(), tc.entries, treeport.Options{Profile: tc.profile, DestinationRoot: tc.root})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "incompatible" || len(r.Conflicts) != 1 || r.Conflicts[0].Status != "incompatible" {
				t.Fatalf("uncertain third member concealed a definite collision: %+v", r)
			}
		})
	}
}

func TestReviewInvalidUTF8DoesNotHideValidAncestors(t *testing.T) {
	t.Run("parent_case_collision", func(t *testing.T) {
		r, err := treeport.Check(context.Background(), []treeport.Entry{
			{Path: "A/\xff"}, {Path: "a/ok"},
		}, treeport.Options{Profile: "windows", DestinationRoot: `C:\dst`})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "incompatible" || len(r.Conflicts) != 1 || r.Conflicts[0].Status != "incompatible" {
			t.Fatalf("invalid leaf hid definite parent alias: %+v", r)
		}
	})
	t.Run("reserved_parent", func(t *testing.T) {
		r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: "CON/\xff"}}, treeport.Options{
			Profile: "windows", DestinationRoot: `C:\dst`,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, issue := range r.Issues {
			found = found || issue.Code == "reserved_name"
		}
		if r.Status != "incompatible" || !found {
			t.Fatalf("invalid leaf hid reserved ASCII parent: %+v", r)
		}
	})
}

func TestReviewTruncationIntroducesTrailingDotOrSpace(t *testing.T) {
	for _, path := range []string{"foo.bar", "foo bar"} {
		r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: path}}, treeport.Options{
			Profile: "export-fold", MaxComponent: 4,
		})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, issue := range r.Issues {
			found = found || issue.Code == "trailing_dot_space"
		}
		if r.Status != "incompatible" || !found {
			t.Fatalf("truncation introduced an invalid ending for %q: %+v", path, r)
		}
	}
}

func TestReviewDeepIndexBudgetReturnsIncomplete(t *testing.T) {
	r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: strings.Repeat("a/", 64) + "leaf"}}, treeport.Options{
		Profile: "posix", Limits: treeport.Limits{MaxIndexBytes: 1024},
	})
	if err == nil || !strings.Contains(err.Error(), "index byte budget") {
		t.Fatalf("deep prefix index did not hit bounded allocation estimate: %v", err)
	}
	if r.Complete || r.Status != "unknown" {
		t.Fatalf("failed check must not present a complete compatibility result: %+v", r)
	}
}
