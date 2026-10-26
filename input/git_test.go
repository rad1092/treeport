package input

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rad1092/treeport"
)

func TestGitReadsTheIndexNotTheWorkingTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "core.precomposeunicode", "false")
	for _, name := range []string{"README.md", "docs/Guide.md", "src/한.txt"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("README.md", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "-A")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := Git(context.Background(), dir, treeport.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, entry := range entries {
		kinds[entry.Path] = entry.Kind
	}
	if _, ok := kinds["untracked.txt"]; ok {
		t.Fatal("untracked files are not in the index")
	}
	if kinds["docs/Guide.md"] != "file" || kinds["src/한.txt"] != "file" {
		t.Fatalf("entries = %v", kinds)
	}
	if runtime.GOOS != "windows" && kinds["link"] != "symlink" {
		t.Fatalf("symlink kind = %q", kinds["link"])
	}

	if _, err := Git(context.Background(), t.TempDir(), treeport.DefaultLimits()); err == nil {
		t.Fatal("a directory outside a repository is an error")
	}
}
