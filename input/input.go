// Package input reads source names without renaming, copying, following tree
// symlinks, or decompressing archive members. It returns no partial result on error.
package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/rad1092/treeport"
)

// ErrLimit identifies an input which exceeded a configured resource budget.
var ErrLimit = errors.New("input limit exceeded")

// ErrInvalidInput identifies malformed or unsupported input structure.
var ErrInvalidInput = errors.New("invalid input")

type budget struct {
	limits  treeport.Limits
	entries []treeport.Entry
	bytes   int64
}

func newBudget(l treeport.Limits) (*budget, error) {
	var err error
	l, err = treeport.NormalizeLimits(l)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return &budget{limits: l}, nil
}

func (b *budget) add(e treeport.Entry) error {
	if len(b.entries) >= b.limits.MaxEntries {
		return fmt.Errorf("%w: MaxEntries=%d", ErrLimit, b.limits.MaxEntries)
	}
	if len(e.Path) > b.limits.MaxPathBytes {
		return fmt.Errorf("%w: MaxPathBytes=%d", ErrLimit, b.limits.MaxPathBytes)
	}
	if int64(len(e.Path)) > b.limits.MaxTotalBytes-b.bytes {
		return fmt.Errorf("%w: MaxTotalBytes=%d", ErrLimit, b.limits.MaxTotalBytes)
	}
	b.bytes += int64(len(e.Path))
	b.entries = append(b.entries, e)
	return nil
}

func checkedContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidInput)
	}
	return ctx.Err()
}

// encodedBudget permits JSON escaping/base64 plus bounded object/ZIP metadata
// overhead, while keeping budgets safe from integer overflow.
func encodedBudget(l treeport.Limits) int64 {
	if l.MaxTotalBytes > (math.MaxInt64-1024)/8 {
		return math.MaxInt64
	}
	n := l.MaxTotalBytes*8 + 1024
	if int64(l.MaxEntries) > (math.MaxInt64-n)/256 {
		return math.MaxInt64
	}
	return n + int64(l.MaxEntries)*256
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	if e := r.ctx.Err(); e != nil {
		return n, e
	}
	return n, err
}
