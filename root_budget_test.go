package treeport_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rad1092/treeport"
)

// Backslashes in POSIX/macOS destination roots are filename bytes. They must
// remain in the full path budget, including immediately before a final slash.
func TestRootBudgetPreservesLiteralBackslashes(t *testing.T) {
	for _, profile := range []string{"posix", "macos"} {
		for _, tc := range []struct {
			name string
			root string
			want int
		}{
			{"one_backslash", `/a\`, 5},
			{"two_backslashes", `/a\\`, 6},
			{"backslash_then_slash", `/a\/`, 5},
			{"two_backslashes_then_slashes", `/a\\//`, 6},
		} {
			t.Run(profile+"/"+tc.name, func(t *testing.T) {
				assertRootBudgetBoundary(t, profile, tc.root, tc.want)
			})
		}
	}
}

// These roots retain their established separator semantics while the literal
// backslash regression is fixed for POSIX/macOS.
func TestRootBudgetOrdinaryAndWindowsSeparators(t *testing.T) {
	for _, tc := range []struct {
		profile string
		name    string
		root    string
		want    int
	}{
		{"posix", "slash_root", "/", 2},
		{"posix", "ordinary_trailing_slash", "/a/", 4},
		{"macos", "slash_root", "/", 2},
		{"macos", "ordinary_trailing_slash", "/a/", 4},
		{"windows", "drive_root", `C:\`, 4},
		{"windows", "drive_directory", `C:\a`, 6},
		{"windows", "drive_directory_trailing_separator", `C:\a\`, 6},
		{"windows", "unc_root", `\\server\share`, 16},
		{"windows", "unc_root_trailing_separator", `\\server\share\`, 16},
		{"export-fold", "backslash_separator", `/a\`, 4},
		{"export-fold", "repeated_mixed_separators", `/a\\//`, 4},
	} {
		t.Run(tc.profile+"/"+tc.name, func(t *testing.T) {
			assertRootBudgetBoundary(t, tc.profile, tc.root, tc.want)
		})
	}
}

func TestRootBudgetEmptyRootHasNoSeparator(t *testing.T) {
	for _, profile := range []string{"posix", "macos", "export-fold"} {
		t.Run(profile, func(t *testing.T) {
			r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: "x", Kind: "file"}}, treeport.Options{
				Profile: profile, DestinationRoot: "", MaxPath: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "known-compatible" || !r.Complete || len(r.Issues) != 0 {
				t.Fatalf("relative x must fit exactly one unit without an invented separator: %+v", r)
			}
		})
	}
}

func assertRootBudgetBoundary(t *testing.T, profile, root string, want int) {
	t.Helper()
	for _, budget := range []int{want - 1, want} {
		t.Run(fmt.Sprintf("budget_%d", budget), func(t *testing.T) {
			r, err := treeport.Check(context.Background(), []treeport.Entry{{Path: "x", Kind: "file"}}, treeport.Options{
				Profile: profile, DestinationRoot: root, MaxPath: budget,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !r.Complete {
				t.Fatal("small root-budget check returned incomplete")
			}
			if r.DestinationRoot != root {
				t.Fatalf("report changed caller root bytes: got %q, want %q", r.DestinationRoot, root)
			}
			var pathIssues []treeport.Issue
			for _, issue := range r.Issues {
				if issue.Code == "path_budget" {
					pathIssues = append(pathIssues, issue)
				}
			}
			if budget < want {
				if r.Status != "incompatible" || len(pathIssues) != 1 {
					t.Fatalf("root %q plus x needs %d units, budget %d incorrectly accepted: %+v", root, want, budget, r)
				}
				wantDetail := fmt.Sprintf("destination root plus path uses %d units; budget %d", want, budget)
				if pathIssues[0].Detail != wantDetail {
					t.Fatalf("wrong measured path length: %q, want %q", pathIssues[0].Detail, wantDetail)
				}
			} else if r.Status != "known-compatible" || len(r.Issues) != 0 {
				t.Fatalf("root %q plus x must fit exact %d-unit boundary: %+v", root, want, r)
			}
		})
	}
}
