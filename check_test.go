package treeport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResourceLimitsAndCancellation(t *testing.T) {
	entries := []Entry{{"a/b/c", "file"}, {"d/e/f", "file"}}
	for _, l := range []Limits{{MaxEntries: 1}, {MaxNodes: 2}, {MaxPathBytes: 2}, {MaxTotalBytes: 4}, {MaxIndexBytes: 200}} {
		if _, err := Check(context.Background(), entries, Options{Profile: "posix", Limits: l}); err == nil {
			t.Errorf("accepted exhausted budget %+v", l)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Check(ctx, nil, Options{Profile: "posix"}); err == nil {
		t.Fatal("canceled context accepted")
	}
	// Exceed diagnostics without permitting a false successful complete report.
	r, err := Check(context.Background(), []Entry{{"NUL", "file"}, {"CON", "file"}}, Options{Profile: "windows", DestinationRoot: `C:\dst`, Limits: Limits{MaxIssues: 1}})
	if err != nil || r.Complete || r.Status != "incompatible" {
		t.Fatalf("bad limited report %+v %v", r, err)
	}
}
func TestShuffleDeterminismAndPreservation(t *testing.T) {
	entries := []Entry{{"Á?/x", "file"}, {"a\u0301*/X", "file"}, {"same", "directory"}, {"same/", "directory"}, {"same/", "directory"}, {"./unsafe", "file"}}
	o := Options{Profile: "export-fold"}
	r, err := Check(context.Background(), entries, o)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(r)
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 30; i++ {
		rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
		got, err := Check(context.Background(), entries, o)
		if err != nil {
			t.Fatal(err)
		}
		j, _ := json.Marshal(got)
		if string(j) != string(want) {
			t.Fatal("enumeration-dependent report")
		}
	}
	var spellings []string
	for _, g := range r.Conflicts {
		for _, m := range g.Members {
			b, _ := base64.StdEncoding.DecodeString(m.Path.Base64)
			if strings.HasPrefix(string(b), "same") {
				spellings = append(spellings, string(b))
			}
		}
	}
	if !reflect.DeepEqual(spellings, []string{"same", "same/"}) {
		t.Fatalf("lost directory name spellings %q", spellings)
	}
}
func TestWindowsRootAndBudget(t *testing.T) {
	for _, root := range []string{`C:relative`, `\\`, `\\server`, `\\server\`, `C:\bad?root`, `C:\..`, `\\?\C:\dst`} {
		if _, err := Check(context.Background(), nil, Options{Profile: "windows", DestinationRoot: root}); err == nil {
			t.Errorf("accepted root %q", root)
		}
	}
	for _, root := range []string{`C:\`, `C:\dst`, `\\server\share`, `\\server\share\dst`} {
		if _, err := Check(context.Background(), nil, Options{Profile: "windows", DestinationRoot: root}); err != nil {
			t.Errorf("rejected root %q: %v", root, err)
		}
	}
	// Unicode full folding may expand names; it must not inflate Win32 path units.
	r, err := Check(context.Background(), []Entry{{"ﬃ", "file"}}, Options{Profile: "windows", DestinationRoot: `C:\`, MaxComponent: 1, MaxPath: 4})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "unknown" {
		t.Fatalf("candidate fold expanded physical budget: %+v", r)
	}
}
func FuzzCheckDeterministic(f *testing.F) {
	for _, s := range []string{"é/e\u0301", "Ａ/한글😀", "COM¹.txt", "../escape", "A./x", string([]byte{0xff, 'a'})} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if len(path) > 1024 {
			t.Skip()
		}
		entries := []Entry{{path, "file"}, {strings.ToUpper(path), "directory"}}
		o := Options{Profile: "export-fold"}
		a, e1 := Check(context.Background(), entries, o)
		entries[0], entries[1] = entries[1], entries[0]
		b, e2 := Check(context.Background(), entries, o)
		if (e1 == nil) != (e2 == nil) {
			t.Fatal("inconsistent errors")
		}
		if e1 == nil {
			x, _ := json.Marshal(a)
			y, _ := json.Marshal(b)
			if string(x) != string(y) {
				t.Fatal("nondeterministic")
			}
		}
	})
}
func TestMillionEntries(t *testing.T) {
	if os.Getenv("TREEPORT_STRESS") != "1" {
		t.Skip("set TREEPORT_STRESS=1 for generated million-entry check")
	}
	entries := make([]Entry, 1000000)
	for i := range entries {
		entries[i] = Entry{fmt.Sprintf("group%04d/item%07d.txt", i/1000, i), "file"}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	r, err := Check(context.Background(), entries, Options{Profile: "export-fold"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if r.Status != "known-compatible" || r.NodeCount != 1001000 {
		t.Fatalf("unexpected million report: status %s nodes %d", r.Status, r.NodeCount)
	}
	t.Logf("STRESS entries=%d nodes=%d elapsed=%s total_alloc_delta_bytes=%d heap_alloc_bytes=%d heap_sys_bytes=%d", r.EntryCount, r.NodeCount, time.Since(start), after.TotalAlloc-before.TotalAlloc, after.HeapAlloc, after.HeapSys)
}

func TestExactInvalidUTF8Duplicates(t *testing.T) {
	p := string([]byte{0xff, 'x'})
	r, err := Check(context.Background(), []Entry{{p, "file"}, {p, "file"}}, Options{Profile: "windows", DestinationRoot: `C:\dst`})
	if err != nil || r.Status != "incompatible" || len(r.Conflicts) != 1 {
		t.Fatalf("lost exact byte duplicate: %+v %v", r, err)
	}
	b, err := base64.StdEncoding.DecodeString(r.Conflicts[0].Members[0].Path.Base64)
	if err != nil || string(b) != p {
		t.Fatal("lost byte identity")
	}
}

func TestInvalidDirectorySpellings(t *testing.T) {
	p := string([]byte{0xff, 'd'})
	r, err := Check(context.Background(), []Entry{{p, "directory"}, {p + "/", "directory"}}, Options{Profile: "export-fold"})
	if err != nil || len(r.Conflicts) != 1 || len(r.Conflicts[0].Members) != 2 {
		t.Fatalf("lost invalid-byte spelling variants: %+v %v", r, err)
	}
}

func TestConsoleDevicesAndSpacedReservedBases(t *testing.T) {
	for _, profile := range []string{"windows", "export-fold"} {
		for _, name := range []string{"CONIN$", "conout$", "CONIN$.log", "CON .txt", "LPT1 .txt", "COM¹ .txt"} {
			t.Run(profile+"/"+name, func(t *testing.T) {
				r, err := Check(context.Background(), []Entry{{name, "file"}}, Options{Profile: profile, DestinationRoot: `C:\dst`})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, issue := range r.Issues {
					found = found || issue.Code == "reserved_name"
				}
				if !found || r.Status != "incompatible" || r.ProfileVersion != "2" {
					t.Fatalf("missed conservative device policy %+v", r)
				}
			})
		}
	}
	for _, name := range []string{"CONIN$extra", "COM10.txt", "console.txt"} {
		r, err := Check(context.Background(), []Entry{{name, "file"}}, Options{Profile: "windows", DestinationRoot: `C:\dst`})
		if err != nil || r.Status != "known-compatible" {
			t.Fatalf("false reserved name %q: %+v %v", name, r, err)
		}
	}
}
