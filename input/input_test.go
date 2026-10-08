package input

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rad1092/treeport"
)

func TestManifestPreservesNames(t *testing.T) {
	raw := string([]byte{'r', 0xff, 'w'})
	in := `{"path":"한글/😀","kind":"file"}` + "\n" + `{"path":"a/e\u0301","kind":"directory"}` + "\n" + `{"path_base64":"` + base64.StdEncoding.EncodeToString([]byte(raw)) + `","kind":"symlink"}` + "\n" + `{"path":"\uD83D\uDE00"}`
	got, err := Manifest(context.Background(), strings.NewReader(in), treeport.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := []treeport.Entry{{Path: "한글/😀", Kind: "file"}, {Path: "a/e\u0301", Kind: "directory"}, {Path: raw, Kind: "symlink"}, {Path: "😀", Kind: "file"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestManifestRejectsMalformed(t *testing.T) {
	cases := []string{
		``, ` `, `[]`, `null`, `{"path":null}`, `{"path":1}`,
		`{"path":"x","kind":null}`, `{"path":"x","kind":"pipe"}`,
		`{"path":"a","path":"b"}`, `{"path":"a","\u0070ath":"b"}`,
		`{"path":"a","wat":"b"}`, `{"kind":"file"}`,
		`{"path":"a","path_base64":"YQ=="}`,
		`{"path_base64":"YQ"}`, `{"path_base64":"YR=="}`, `{"path_base64":"YQ==\n"}`,
		`{"path":"\ud800"}`, `{"path":"\udc00"}`, `{"path":"\ud800\u0041"}`,
		`{"path":"\ud800\ud800"}`, `{"path":"\uXYZW"}`, `{"path":"\u1"}`,
		`{"path":"x"} {"path":"y"}`, `{"path":"x",}`, "{\"path\":\"\xff\"}",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			// Empty input is a valid empty manifest; an empty JSONL line is not.
			if s == "" {
				s = "\n"
			}
			got, err := Manifest(context.Background(), strings.NewReader(s), treeport.Limits{})
			if !errors.Is(err, ErrInvalidInput) || got != nil {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
	if _, err := Manifest(context.Background(), strings.NewReader(`{"path":"literal\\ud800"}`), treeport.Limits{}); err != nil {
		t.Fatal(err)
	}
	if got, err := Manifest(context.Background(), strings.NewReader(""), treeport.Limits{}); err != nil || len(got) != 0 {
		t.Fatalf("empty manifest: %v %v", got, err)
	}
}

func TestManifestLimitsAndErrors(t *testing.T) {
	cases := []struct {
		name, input string
		limits      treeport.Limits
	}{
		{"entries", `{"path":"a"}` + "\n" + `{"path":"b"}`, treeport.Limits{MaxEntries: 1}},
		{"path", `{"path":"abcd"}`, treeport.Limits{MaxPathBytes: 3}},
		{"total", `{"path":"ab"}` + "\n" + `{"path":"cd"}`, treeport.Limits{MaxTotalBytes: 3}},
		{"line", `{"path":"` + strings.Repeat("a", 2048) + `"}`, treeport.Limits{MaxPathBytes: 1}},
		{"encoded", `{"path":"a"}` + strings.Repeat(" ", 1500), treeport.Limits{MaxEntries: 1, MaxTotalBytes: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Manifest(context.Background(), strings.NewReader(tc.input), tc.limits)
			if !errors.Is(err, ErrLimit) || got != nil {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Manifest(ctx, strings.NewReader(`{"path":"x"}`), treeport.Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Manifest(context.Background(), badReader{}, treeport.Limits{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if _, err := Manifest(nil, strings.NewReader(""), treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := Manifest(context.Background(), nil, treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := Manifest(context.Background(), strings.NewReader(""), treeport.Limits{MaxEntries: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestTreeReadOnlyAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "dir", "한글😀")
	if err := os.WriteFile(file, []byte("contents must not change"), 0640); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	modeBefore, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	symlink := os.Symlink(root, filepath.Join(root, "loop")) == nil
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	external := os.Symlink(out, filepath.Join(root, "external")) == nil
	t.Logf("native symlink support exercised: loop=%v external=%v", symlink, external)
	got, err := Tree(context.Background(), root, treeport.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := []treeport.Entry{{Path: "dir", Kind: "directory"}, {Path: "dir/한글😀", Kind: "file"}}
	if symlink {
		want = append(want, treeport.Entry{Path: "loop", Kind: "symlink"})
	}
	if external {
		want = append(want, treeport.Entry{Path: "external", Kind: "symlink"})
	}
	sortEntries(got)
	sortEntries(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	modeAfter, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || modeBefore.Mode() != modeAfter.Mode() || !modeBefore.ModTime().Equal(modeAfter.ModTime()) {
		t.Fatal("source content, mode or mtime changed")
	}
	if symlink {
		if _, err := Tree(context.Background(), filepath.Join(root, "loop"), treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
		link, err := os.Readlink(filepath.Join(root, "loop"))
		if err != nil || link != root {
			t.Fatalf("link changed: %q %v", link, err)
		}
	}
}

func TestTreeBatchesAndLimits(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 600; i++ {
		f, err := os.CreateTemp(root, "name-")
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Tree(context.Background(), root, treeport.Limits{})
	if err != nil || len(got) != 600 {
		t.Fatalf("entries=%d err=%v", len(got), err)
	}
	for _, l := range []treeport.Limits{{MaxEntries: 500}, {MaxTotalBytes: 20}, {MaxPathBytes: 2}} {
		got, err := Tree(context.Background(), root, l)
		if !errors.Is(err, ErrLimit) || got != nil {
			t.Fatalf("got %d entries, %v", len(got), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Tree(ctx, root, treeport.Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTreePermissionErrorFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permission model is not applicable on Windows")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0700) })
	if f, err := os.Open(locked); err == nil {
		_ = f.Close()
		t.Skip("current account bypasses directory permission bits")
	}
	got, err := Tree(context.Background(), root, treeport.Limits{})
	if err == nil || got != nil {
		t.Fatalf("permission failure returned partial/success result: %#v %v", got, err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected permission error: %v", err)
	}
}

func sortEntries(e []treeport.Entry) {
	sort.Slice(e, func(i, j int) bool { return e[i].Path < e[j].Path })
}

func TestTreeRawInvalidUTF8(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows APIs expose UTF-16 names, not arbitrary byte names")
	}
	root := t.TempDir()
	name := string([]byte{'n', 0xff})
	if err := os.WriteFile(filepath.Join(root, name), []byte("content"), 0600); err != nil {
		t.Skipf("host filesystem does not accept an invalid-UTF-8 name: %v", err)
	}
	got, err := Tree(context.Background(), root, treeport.Limits{})
	if err != nil || len(got) != 1 || got[0].Path != name {
		t.Fatalf("raw bytes not preserved: %#v %v", got, err)
	}
}

func TestManifestCancellationDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancelReader{cancel: cancel}
	got, err := Manifest(ctx, r, treeport.Limits{})
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("got %#v %v", got, err)
	}
}

type cancelReader struct {
	count  int
	cancel context.CancelFunc
}

func (r *cancelReader) Read(p []byte) (int, error) {
	r.count++
	if r.count == 8 {
		r.cancel()
	}
	return copy(p, "{\"path\":\"file\"}\n"), nil
}

// Million-entry benchmark fixtures are streamed and never written to disk.
func BenchmarkManifestMillion(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := &generatedManifest{remaining: 1_000_000}
		entries, err := Manifest(context.Background(), r, treeport.Limits{})
		if err != nil || len(entries) != 1_000_000 {
			b.Fatalf("entries=%d, err=%v", len(entries), err)
		}
	}
}

type generatedManifest struct {
	remaining int
	pending   []byte
}

func (r *generatedManifest) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.remaining == 0 {
			return 0, io.EOF
		}
		r.pending = append(r.pending, "{\"path\":\"file-"...)
		r.pending = strconv.AppendInt(r.pending, int64(r.remaining), 10)
		r.pending = append(r.pending, "\"}\n"...)
		r.remaining--
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func zipFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		mode os.FileMode
	}{{"dir/", os.ModeDir | 0755}, {"dir/file", 0644}, {"../outside", 0644}, {"/absolute", 0644}, {"same", 0644}, {"same", os.ModeDir | 0755}, {"link", os.ModeSymlink | 0777}, {string([]byte{'b', 0xff}), 0644}} {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, NonUTF8: true}
		h.SetMode(entry.mode)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if entry.mode&os.ModeDir == 0 {
			if _, err = f.Write([]byte("payload")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestZIPMetadataPreservesUnsafeAndDuplicateNames(t *testing.T) {
	data := zipFixture(t)
	filename := filepath.Join(t.TempDir(), "input.zip")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	got, err := ZIP(context.Background(), filename, treeport.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := []treeport.Entry{{Path: "dir/", Kind: "directory"}, {Path: "dir/file", Kind: "file"}, {Path: "../outside", Kind: "file"}, {Path: "/absolute", Kind: "file"}, {Path: "same", Kind: "file"}, {Path: "same", Kind: "directory"}, {Path: "link", Kind: "symlink"}, {Path: string([]byte{'b', 0xff}), Kind: "file"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	after, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if before != sha256.Sum256(after) {
		t.Fatal("archive changed")
	}
	files, err := os.ReadDir(filepath.Dir(filename))
	if err != nil || len(files) != 1 {
		t.Fatalf("archive extracted files: %v %v", files, err)
	}
}

func TestZIPLimitsAndDishonestCounts(t *testing.T) {
	data := zipFixture(t)
	for _, l := range []treeport.Limits{{MaxEntries: 2}, {MaxPathBytes: 2}, {MaxTotalBytes: 3}} {
		got, err := zipReader(context.Background(), bytes.NewReader(data), int64(len(data)), l)
		if !errors.Is(err, ErrLimit) || got != nil {
			t.Fatalf("got %v %v", got, err)
		}
	}
	// A dishonest EOCD count must not bypass actual-record accounting.
	for _, limit := range []int{2, 20} {
		fake := append([]byte(nil), data...)
		binary.LittleEndian.PutUint16(fake[len(fake)-22+8:], 1)
		binary.LittleEndian.PutUint16(fake[len(fake)-22+10:], 1)
		got, err := zipReader(context.Background(), bytes.NewReader(fake), int64(len(fake)), treeport.Limits{MaxEntries: limit})
		wantErr := ErrInvalidInput
		if limit == 2 {
			wantErr = ErrLimit
		}
		if !errors.Is(err, wantErr) || got != nil {
			t.Fatalf("limit %d got %#v %v", limit, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := zipReader(ctx, bytes.NewReader(data), int64(len(data)), treeport.Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func zip64Fixture(t *testing.T) []byte {
	t.Helper()
	data := zipFixture(t)
	oldEnd := data[len(data)-22:]
	pos := len(data) - 22
	z := make([]byte, 56+20+22)
	binary.LittleEndian.PutUint32(z, zip64EndSignature)
	binary.LittleEndian.PutUint64(z[4:], 44)
	binary.LittleEndian.PutUint16(z[12:], 45)
	binary.LittleEndian.PutUint16(z[14:], 45)
	binary.LittleEndian.PutUint64(z[24:], 8)
	binary.LittleEndian.PutUint64(z[32:], 8)
	binary.LittleEndian.PutUint64(z[40:], uint64(binary.LittleEndian.Uint32(oldEnd[12:])))
	binary.LittleEndian.PutUint64(z[48:], uint64(binary.LittleEndian.Uint32(oldEnd[16:])))
	binary.LittleEndian.PutUint32(z[56:], zip64LocatorSignature)
	binary.LittleEndian.PutUint64(z[64:], uint64(pos))
	binary.LittleEndian.PutUint32(z[72:], 1)
	copy(z[76:], oldEnd)
	binary.LittleEndian.PutUint16(z[76+8:], 0xffff)
	binary.LittleEndian.PutUint16(z[76+10:], 0xffff)
	binary.LittleEndian.PutUint32(z[76+12:], 0xffffffff)
	binary.LittleEndian.PutUint32(z[76+16:], 0xffffffff)
	return append(append([]byte(nil), data[:pos]...), z...)
}

func TestZIP64(t *testing.T) {
	data := zip64Fixture(t)
	got, err := zipReader(context.Background(), bytes.NewReader(data), int64(len(data)), treeport.Limits{})
	if err != nil || len(got) != 8 {
		t.Fatalf("got %d entries: %v", len(got), err)
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint64(b[len(b)-42+8:], ^uint64(0)) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[len(b)-98+4:], ^uint64(0)) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-98+16:], 1) },
	} {
		bad := append([]byte(nil), data...)
		mutate(bad)
		if _, err := zipReader(context.Background(), bytes.NewReader(bad), int64(len(bad)), treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
}

func TestZIPMalformedRecords(t *testing.T) {
	data := zipFixture(t)
	for _, input := range [][]byte{nil, data[:21], append(append([]byte(nil), data...), 1)} {
		if _, err := zipReader(context.Background(), bytes.NewReader(input), int64(len(input)), treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-22+16:], 0xffffffff-1) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[len(b)-22+4:], 1) },
		func(b []byte) { offset := binary.LittleEndian.Uint32(b[len(b)-22+16:]); b[offset] = 0 },
		func(b []byte) {
			offset := binary.LittleEndian.Uint32(b[len(b)-22+16:])
			binary.LittleEndian.PutUint16(b[offset+30:], 0xffff)
		},
	} {
		bad := append([]byte(nil), data...)
		mutate(bad)
		if _, err := zipReader(context.Background(), bytes.NewReader(bad), int64(len(bad)), treeport.Limits{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
}

func FuzzManifest(f *testing.F) {
	for _, seed := range []string{`{"path":"a"}`, `{"path":"\ud800"}`, `{"path_base64":"/w=="}`, `{"path":"x","path":"y"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := Manifest(context.Background(), strings.NewReader(s), treeport.Limits{MaxEntries: 16, MaxPathBytes: 128, MaxTotalBytes: 1024})
		if err != nil && got != nil {
			t.Fatal("partial result on error")
		}
		if len(got) > 16 {
			t.Fatal("entry limit bypass")
		}
	})
}

func FuzzZIP(f *testing.F) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	_, _ = w.Create("a")
	_ = w.Close()
	f.Add(buf.Bytes())
	f.Add([]byte("bad ZIP"))
	f.Fuzz(func(t *testing.T, b []byte) {
		got, err := zipReader(context.Background(), bytes.NewReader(b), int64(len(b)), treeport.Limits{MaxEntries: 16, MaxPathBytes: 128, MaxTotalBytes: 1024})
		if err != nil && got != nil {
			t.Fatal("partial result on error")
		}
		if len(got) > 16 {
			t.Fatal("entry limit bypass")
		}
	})
}
