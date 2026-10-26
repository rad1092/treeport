package input

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rad1092/treeport"
)

// Git reads the names in a repository's index with `git ls-files -z --stage`,
// the names a clone on another machine will create. It runs git read-only in
// dir and never touches the working tree. Symlinks (mode 120000) are symlink
// entries and submodules (mode 160000) are directories.
func Git(ctx context.Context, dir string, limits treeport.Limits) ([]treeport.Entry, error) {
	if err := checkedContext(ctx); err != nil {
		return nil, err
	}
	b, err := newBudget(limits)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-z", "--stage", "--full-name")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: git is not installed or not on PATH", ErrInvalidInput)
		}
		return nil, err
	}
	reader := bufio.NewReaderSize(contextReader{ctx, stdout}, 64<<10)
	seen := map[string]bool{}
	var parseErr error
	for {
		record, readErr := reader.ReadString(0)
		if readErr != nil && record == "" {
			break
		}
		record = strings.TrimSuffix(record, "\x00")
		// "<mode> <object> <stage>\t<path>"
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || path == "" {
			parseErr = fmt.Errorf("%w: unexpected git ls-files record %q", ErrInvalidInput, record)
			break
		}
		// Unmerged paths appear once per conflict stage; they are one name.
		if seen[path] {
			continue
		}
		seen[path] = true
		kind := "file"
		switch fields[0] {
		case "120000":
			kind = "symlink"
		case "160000":
			kind = "directory"
		}
		if err := b.add(treeport.Entry{Path: path, Kind: kind}); err != nil {
			parseErr = err
			break
		}
		if readErr != nil {
			break
		}
	}
	if parseErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, parseErr
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git ls-files in %s: %s", dir, message)
	}
	return b.entries, nil
}
