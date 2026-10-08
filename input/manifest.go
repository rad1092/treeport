package input

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"github.com/rad1092/treeport"
)

// Manifest reads strict JSONL objects: path or path_base64, and optional kind
// (default file). Duplicate/unknown fields, invalid UTF-8 and unpaired JSON
// surrogate escapes are rejected. path_base64 preserves arbitrary path bytes.
// An arbitrary blocked io.Reader cannot be interrupted by context alone; callers
// should close a pipe or network reader on cancellation when they own it.
func Manifest(ctx context.Context, r io.Reader, limits treeport.Limits) ([]treeport.Entry, error) {
	if err := checkedContext(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil reader", ErrInvalidInput)
	}
	b, err := newBudget(limits)
	if err != nil {
		return nil, err
	}
	if b.limits.MaxPathBytes > (math.MaxInt-1025)/8 {
		return nil, fmt.Errorf("%w: MaxPathBytes too large for line budget", ErrInvalidInput)
	}
	lineCap := b.limits.MaxPathBytes*8 + 1024
	reader := &io.LimitedReader{R: contextReader{ctx, r}, N: encodedBudget(b.limits)}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(64<<10, lineCap+1)), lineCap+1)
	line := 0
	for scanner.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if reader.N == 0 {
			return nil, fmt.Errorf("%w: encoded manifest byte budget", ErrLimit)
		}
		if len(b.entries) >= b.limits.MaxEntries {
			return nil, fmt.Errorf("%w: MaxEntries=%d at line %d", ErrLimit, b.limits.MaxEntries, line)
		}
		raw := scanner.Bytes()
		if len(raw) > lineCap {
			return nil, fmt.Errorf("%w: manifest line %d exceeds %d bytes", ErrLimit, line, lineCap)
		}
		e, err := decodeEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("manifest line %d: %w", line, err)
		}
		if err = b.add(e); err != nil {
			return nil, fmt.Errorf("manifest line %d: %w", line, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return nil, fmt.Errorf("%w: manifest line exceeds %d bytes", ErrLimit, lineCap)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if reader.N == 0 {
		return nil, fmt.Errorf("%w: encoded manifest byte budget", ErrLimit)
	}
	return b.entries, nil
}

func decodeEntry(raw []byte) (treeport.Entry, error) {
	var e treeport.Entry
	if !utf8.Valid(raw) {
		return e, fmt.Errorf("%w: JSON must be UTF-8; use path_base64 for raw bytes", ErrInvalidInput)
	}
	if err := validSurrogates(raw); err != nil {
		return e, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil {
		return e, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if t != json.Delim('{') {
		return e, fmt.Errorf("%w: expected object", ErrInvalidInput)
	}
	seen := make(map[string]bool, 3)
	var path, encoded string
	e.Kind = "file"
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return e, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		key, ok := t.(string)
		if !ok {
			return e, fmt.Errorf("%w: expected field name", ErrInvalidInput)
		}
		if seen[key] {
			return e, fmt.Errorf("%w: duplicate field %q", ErrInvalidInput, key)
		}
		seen[key] = true
		if key != "path" && key != "path_base64" && key != "kind" {
			return e, fmt.Errorf("%w: unknown field %q", ErrInvalidInput, key)
		}
		t, err = d.Token()
		if err != nil {
			return e, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		value, ok := t.(string)
		if !ok {
			return e, fmt.Errorf("%w: field %q must be a string", ErrInvalidInput, key)
		}
		switch key {
		case "path":
			path = value
		case "path_base64":
			encoded = value
		case "kind":
			e.Kind = value
		}
	}
	if t, err = d.Token(); err != nil || t != json.Delim('}') {
		return e, fmt.Errorf("%w: malformed object", ErrInvalidInput)
	}
	if _, err = d.Token(); err != io.EOF {
		return e, fmt.Errorf("%w: trailing JSON content", ErrInvalidInput)
	}
	if seen["path"] == seen["path_base64"] {
		return e, fmt.Errorf("%w: exactly one of path and path_base64 is required", ErrInvalidInput)
	}
	if e.Kind != "file" && e.Kind != "directory" && e.Kind != "symlink" {
		return e, fmt.Errorf("%w: unsupported kind %q", ErrInvalidInput, e.Kind)
	}
	if seen["path_base64"] {
		p, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || bytes.IndexAny([]byte(encoded), "\r\n") >= 0 {
			return e, fmt.Errorf("%w: invalid canonical base64", ErrInvalidInput)
		}
		path = string(p)
	}
	e.Path = path
	return e, nil
}

// Validate JSON string escapes before encoding/json can replace malformed UTF-16
// surrogate escapes with U+FFFD. Full JSON syntax is checked by its decoder.
func validSurrogates(raw []byte) error {
	inside := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			break
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return fmt.Errorf("%w: short Unicode escape", ErrInvalidInput)
		}
		u, ok := hex4(raw[i+1 : i+5])
		if !ok {
			return fmt.Errorf("%w: invalid Unicode escape", ErrInvalidInput)
		}
		i += 4
		if u >= 0xDC00 && u <= 0xDFFF {
			return fmt.Errorf("%w: unpaired low surrogate", ErrInvalidInput)
		}
		if u >= 0xD800 && u <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return fmt.Errorf("%w: unpaired high surrogate", ErrInvalidInput)
			}
			v, ok := hex4(raw[i+3 : i+7])
			if !ok || v < 0xDC00 || v > 0xDFFF {
				return fmt.Errorf("%w: unpaired high surrogate", ErrInvalidInput)
			}
			i += 6
		}
	}
	return nil
}

func hex4(b []byte) (uint16, bool) {
	var n uint16
	for _, c := range b {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			n += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}
