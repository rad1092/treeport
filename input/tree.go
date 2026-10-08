package input

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rad1092/treeport"
)

// Tree enumerates descendants in batches of 256. The source root is excluded.
// Symlinks are entries, never traversal targets; a symlink root is rejected.
// Read/permission errors fail closed. This is not an atomic filesystem snapshot:
// concurrent path replacement and other source races are outside its guarantee.
func Tree(ctx context.Context, root string, limits treeport.Limits) ([]treeport.Entry, error) {
	if err := checkedContext(ctx); err != nil {
		return nil, err
	}
	b, err := newBudget(limits)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stat source root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: source root must be a directory, not a symlink", ErrInvalidInput)
	}
	// Pending directories reuse path strings also stored in the bounded result.
	dirs := []string{""}
	for len(dirs) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rel := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]
		name := filepath.Join(root, filepath.FromSlash(rel))
		before, err := os.Lstat(name)
		if err != nil {
			return nil, fmt.Errorf("stat directory %q: %w", rel, err)
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: directory %q changed during enumeration", ErrInvalidInput, rel)
		}
		f, err := os.Open(name)
		if err != nil {
			return nil, fmt.Errorf("open directory %q: %w", rel, err)
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(before, opened) {
			_ = f.Close()
			if err != nil {
				return nil, fmt.Errorf("stat opened directory %q: %w", rel, err)
			}
			return nil, fmt.Errorf("%w: directory %q changed during enumeration", ErrInvalidInput, rel)
		}
		err = readDirectory(ctx, f, rel, b, &dirs)
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close directory %q: %w", rel, closeErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.entries, nil
}

func readDirectory(ctx context.Context, f *os.File, rel string, b *budget, dirs *[]string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := f.ReadDir(256)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read directory %q: %w", rel, err)
		}
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := item.Name()
			if rel != "" {
				name = rel + "/" + name
			}
			// Info uses lstat semantics and fails closed on inaccessible/replaced names.
			info, e := item.Info()
			if e != nil {
				return fmt.Errorf("stat entry %q: %w", name, e)
			}
			kind := "file"
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				kind = "symlink"
			case info.IsDir():
				kind = "directory"
			case !info.Mode().IsRegular():
				return fmt.Errorf("%w: unsupported special file %q (%s)", ErrInvalidInput, name, info.Mode())
			}
			if err := b.add(treeport.Entry{Path: name, Kind: kind}); err != nil {
				return err
			}
			if kind == "directory" {
				*dirs = append(*dirs, name)
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}
