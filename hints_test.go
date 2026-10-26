package treeport_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rad1092/treeport"
	"golang.org/x/text/unicode/norm"
)

func check(t *testing.T, profile string, paths ...string) treeport.Report {
	t.Helper()
	entries := make([]treeport.Entry, len(paths))
	for i, path := range paths {
		entries[i] = treeport.Entry{Path: path, Kind: "file"}
	}
	opts := treeport.Options{Profile: profile}
	if profile == "windows" {
		opts.DestinationRoot = `C:\dst`
	}
	report, err := treeport.Check(context.Background(), entries, opts)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestDecomposedNamesWarnWithoutChangingStatus(t *testing.T) {
	nfd := norm.NFD.String("보고서") + "/" + norm.NFD.String("한글") + ".txt"
	for _, profile := range []string{"posix", "windows", "export-fold"} {
		report := check(t, profile, nfd, "README.md")
		if len(report.Warnings) != 2 {
			t.Fatalf("%s: warnings = %+v", profile, report.Warnings)
		}
		for _, warning := range report.Warnings {
			if warning.Code != "nfd_name" || warning.Status != "warning" || !strings.Contains(warning.Detail, "NFC spelling") {
				t.Fatalf("%s: warning = %+v", profile, warning)
			}
		}
		if report.Warnings[0].Path.Display != norm.NFD.String("보고서") {
			t.Fatalf("%s: the directory is warned once by its own path: %+v", profile, report.Warnings)
		}
	}
	if report := check(t, "posix", nfd); report.Status != "known-compatible" {
		t.Fatalf("a warning must not change the status: %s", report.Status)
	}
	if report := check(t, "macos", nfd); len(report.Warnings) != 0 {
		t.Fatalf("macOS writes NFD itself: %+v", report.Warnings)
	}
	if report := check(t, "windows", "한글.txt"); len(report.Warnings) != 0 {
		t.Fatalf("NFC names are not warned: %+v", report.Warnings)
	}
}

func TestUnknownResultsCarryALikelyVerdict(t *testing.T) {
	alone := check(t, "windows", "한글.txt", "README.md")
	if alone.Status != "unknown" || alone.Likely != "compatible" {
		t.Fatalf("status %s likely %q", alone.Status, alone.Likely)
	}
	if !strings.Contains(alone.Issues[0].Hint, "very likely works") {
		t.Fatalf("hint = %q", alone.Issues[0].Hint)
	}

	caseOnly := check(t, "windows", "Äpfel.txt", "äpfel.txt")
	if caseOnly.Likely != "incompatible" || len(caseOnly.Conflicts) != 1 || !strings.Contains(caseOnly.Conflicts[0].Hint, "very likely collide") {
		t.Fatalf("likely %q conflicts %+v", caseOnly.Likely, caseOnly.Conflicts)
	}
	for _, issue := range caseOnly.Issues {
		if !strings.Contains(issue.Hint, "Very likely collides") {
			t.Fatalf("issue hint = %q", issue.Hint)
		}
	}

	fullFold := check(t, "windows", "straße.txt", "STRASSE.txt")
	if fullFold.Likely != "compatible" || !strings.Contains(fullFold.Conflicts[0].Hint, "probably different") {
		t.Fatalf("likely %q conflicts %+v", fullFold.Likely, fullFold.Conflicts)
	}

	apfs := check(t, "macos", "café.txt", norm.NFD.String("Café.txt"))
	if apfs.Likely != "incompatible" {
		t.Fatalf("NFC and NFD spellings collide on default APFS: %+v", apfs)
	}

	known := check(t, "posix", "a.txt")
	if known.Likely != "" {
		t.Fatalf("likely is only set for unknown results: %q", known.Likely)
	}
}
