package treeport_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rad1092/treeport"
)

// This corpus was specified before the implementation, from destination rules and
// independently chosen compositions. Expectations intentionally avoid group IDs.
func TestIndependentRegressionCorpus(t *testing.T) {
	var fixtures []struct {
		Name            string `json:"name"`
		Profile         string `json:"profile"`
		DestinationRoot string `json:"destination_root"`
		MaxComponent    int    `json:"max_component"`
		MaxPath         int    `json:"max_path"`
		Entries         []struct {
			Path   string `json:"path"`
			Base64 string `json:"path_base64"`
			Kind   string `json:"kind"`
		} `json:"entries"`
		Status      string   `json:"status"`
		Issues      []string `json:"issues"`
		Causes      []string `json:"causes"`
		NoConflicts bool     `json:"no_conflicts"`
	}
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			entries := make([]treeport.Entry, len(f.Entries))
			for i, e := range f.Entries {
				path := e.Path
				if e.Base64 != "" {
					b, err := base64.StdEncoding.DecodeString(e.Base64)
					if err != nil {
						t.Fatal(err)
					}
					path = string(b)
				}
				kind := e.Kind
				if kind == "" {
					kind = "file"
				}
				entries[i] = treeport.Entry{Path: path, Kind: kind}
			}
			original := slices.Clone(entries)
			opts := treeport.Options{Profile: f.Profile, DestinationRoot: f.DestinationRoot, MaxComponent: f.MaxComponent, MaxPath: f.MaxPath}
			if opts.Profile == "windows" && opts.DestinationRoot == "" {
				opts.DestinationRoot = `C:\dst`
			}
			r, err := treeport.Check(context.Background(), entries, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Complete {
				t.Fatal("small complete corpus returned incomplete report")
			}
			if r.Status != f.Status {
				t.Errorf("status = %q, want %q", r.Status, f.Status)
			}
			if !reflect.DeepEqual(entries, original) {
				t.Error("Check mutated caller's entries")
			}
			if f.NoConflicts && len(r.Conflicts) != 0 {
				t.Errorf("unexpected conflicts: %+v", r.Conflicts)
			}
			for _, want := range f.Issues {
				found := false
				for _, i := range r.Issues {
					found = found || i.Code == want
				}
				if !found {
					t.Errorf("missing issue %s; got %+v", want, r.Issues)
				}
			}
			for _, want := range f.Causes {
				found := false
				for _, c := range r.Conflicts {
					found = found || slices.Contains(c.Causes, want)
				}
				if !found {
					t.Errorf("missing conflict cause %s; got %+v", want, r.Conflicts)
				}
			}
			// Reverse input to exercise enumeration independence without copying the
			// implementation's sorting or canonicalization rules into the test.
			slices.Reverse(entries)
			reversed, err := treeport.Check(context.Background(), entries, opts)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(r)
			b, _ := json.Marshal(reversed)
			if string(a) != string(b) {
				t.Errorf("report depends on enumeration order\n%s\n%s", a, b)
			}
			// Every surfaced original must survive exact-byte round-trip, including
			// invalid UTF-8 and canonically equivalent spellings.
			originalPaths := map[string]bool{}
			for _, entry := range original {
				originalPaths[entry.Path] = true
				for n := 0; n < len(entry.Path); n++ {
					if entry.Path[n] == '/' {
						originalPaths[entry.Path[:n]] = true
					}
				}
			}
			checkOriginal := func(o treeport.Original) {
				t.Helper()
				b, err := base64.StdEncoding.DecodeString(o.Base64)
				if err != nil {
					t.Fatal(err)
				}
				if !originalPaths[string(b)] {
					t.Errorf("original bytes are not an exact source or parent: %+v", o)
				}
				if o.Display != strings.ToValidUTF8(string(b), "\uFFFD") {
					t.Errorf("display changed valid original Unicode spelling: %+v", o)
				}
			}
			for _, i := range r.Issues {
				checkOriginal(i.Path)
			}
			for _, c := range r.Conflicts {
				for _, m := range c.Members {
					checkOriginal(m.Path)
				}
			}
		})
	}
}
