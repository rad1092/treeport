package input

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/rad1092/treeport"
)

const (
	zipCentralSignature   = 0x02014b50
	zipEndSignature       = 0x06054b50
	zip64EndSignature     = 0x06064b50
	zip64LocatorSignature = 0x07064b50
)

// ZIP reads only ZIP/ZIP64 central-directory names and attributes. It never
// decompresses or extracts, validates payloads/CRCs, or trusts advertised entry
// counts as the only limit. Raw name bytes (including directory '/' suffixes,
// traversal, duplicates and invalid UTF-8) survive for the checker to diagnose.
// Split, encrypted-central-directory and nonstandard-offset archives are rejected.
func ZIP(ctx context.Context, filename string, limits treeport.Limits) ([]treeport.Entry, error) {
	if err := checkedContext(ctx); err != nil {
		return nil, err
	}
	// Reject pipes/devices before opening: opening a FIFO may block indefinitely.
	// The post-open stat also catches ordinary concurrent replacement; atomic
	// protection from hostile filesystem races is outside this reader's contract.
	before, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("stat ZIP source: %w", err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: ZIP source must be a regular file", ErrInvalidInput)
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat ZIP: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, fmt.Errorf("%w: ZIP source must be a regular file", ErrInvalidInput)
	}
	return zipReader(ctx, f, info.Size(), limits)
}

func zipReader(ctx context.Context, r io.ReaderAt, size int64, limits treeport.Limits) ([]treeport.Entry, error) {
	if err := checkedContext(ctx); err != nil {
		return nil, err
	}
	b, err := newBudget(limits)
	if err != nil {
		return nil, err
	}
	if size < 22 {
		return nil, fmt.Errorf("%w: ZIP end record missing", ErrInvalidInput)
	}
	tail := make([]byte, int(min(size, 22+65535)))
	if err := readAt(ctx, r, tail, size-int64(len(tail))); err != nil {
		return nil, fmt.Errorf("read ZIP tail: %w", err)
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == zipEndSignature && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("%w: ZIP end record missing or trailing data", ErrInvalidInput)
	}
	e := tail[end:]
	eocdOffset := size - int64(len(tail)) + int64(end)
	disk := binary.LittleEndian.Uint16(e[4:])
	cdDisk := binary.LittleEndian.Uint16(e[6:])
	countDisk := binary.LittleEndian.Uint16(e[8:])
	count := uint64(binary.LittleEndian.Uint16(e[10:]))
	cdSize := uint64(binary.LittleEndian.Uint32(e[12:]))
	cdOffset := uint64(binary.LittleEndian.Uint32(e[16:]))
	if disk != 0 || cdDisk != 0 || uint64(countDisk) != count {
		return nil, fmt.Errorf("%w: split ZIP archives are unsupported", ErrInvalidInput)
	}
	metadataStart := uint64(eocdOffset)
	if count == math.MaxUint16 || cdSize == math.MaxUint32 || cdOffset == math.MaxUint32 {
		if eocdOffset < 20 {
			return nil, fmt.Errorf("%w: ZIP64 locator missing", ErrInvalidInput)
		}
		var locator [20]byte
		if err := readAt(ctx, r, locator[:], eocdOffset-20); err != nil {
			return nil, fmt.Errorf("read ZIP64 locator: %w", err)
		}
		if binary.LittleEndian.Uint32(locator[:]) != zip64LocatorSignature {
			return nil, fmt.Errorf("%w: ZIP64 locator missing", ErrInvalidInput)
		}
		if binary.LittleEndian.Uint32(locator[4:]) != 0 || binary.LittleEndian.Uint32(locator[16:]) != 1 {
			return nil, fmt.Errorf("%w: split ZIP64 archives are unsupported", ErrInvalidInput)
		}
		zip64Offset := binary.LittleEndian.Uint64(locator[8:])
		if zip64Offset > uint64(eocdOffset-20) || uint64(eocdOffset-20)-zip64Offset < 56 {
			return nil, fmt.Errorf("%w: ZIP64 end record out of bounds", ErrInvalidInput)
		}
		var z [56]byte
		if err := readAt(ctx, r, z[:], int64(zip64Offset)); err != nil {
			return nil, fmt.Errorf("read ZIP64 end: %w", err)
		}
		if binary.LittleEndian.Uint32(z[:]) != zip64EndSignature {
			return nil, fmt.Errorf("%w: ZIP64 end signature missing", ErrInvalidInput)
		}
		recordSize := binary.LittleEndian.Uint64(z[4:])
		if recordSize < 44 || recordSize > uint64(eocdOffset-20)-zip64Offset-12 {
			return nil, fmt.Errorf("%w: ZIP64 end record size invalid", ErrInvalidInput)
		}
		if binary.LittleEndian.Uint32(z[16:]) != 0 || binary.LittleEndian.Uint32(z[20:]) != 0 || binary.LittleEndian.Uint64(z[24:]) != binary.LittleEndian.Uint64(z[32:]) {
			return nil, fmt.Errorf("%w: split ZIP64 archives are unsupported", ErrInvalidInput)
		}
		count = binary.LittleEndian.Uint64(z[32:])
		cdSize = binary.LittleEndian.Uint64(z[40:])
		cdOffset = binary.LittleEndian.Uint64(z[48:])
		metadataStart = zip64Offset
	}
	if count > uint64(b.limits.MaxEntries) {
		return nil, fmt.Errorf("%w: ZIP advertises %d entries, MaxEntries=%d", ErrLimit, count, b.limits.MaxEntries)
	}
	if cdSize > uint64(encodedBudget(b.limits)) {
		return nil, fmt.Errorf("%w: ZIP central directory exceeds encoded metadata byte budget", ErrLimit)
	}
	if cdOffset > metadataStart || cdSize > metadataStart-cdOffset {
		return nil, fmt.Errorf("%w: ZIP central directory out of bounds", ErrInvalidInput)
	}
	// A bounded count cannot force a giant allocation: records are read one at a
	// time and extra/comment payloads are skipped through offsets, never allocated.
	position := cdOffset
	endPosition := cdOffset + cdSize
	for position < endPosition {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(b.entries) >= b.limits.MaxEntries {
			return nil, fmt.Errorf("%w: actual ZIP entries exceed MaxEntries=%d", ErrLimit, b.limits.MaxEntries)
		}
		if endPosition-position < 46 {
			return nil, fmt.Errorf("%w: truncated ZIP central record", ErrInvalidInput)
		}
		var h [46]byte
		if err := readAt(ctx, r, h[:], int64(position)); err != nil {
			return nil, fmt.Errorf("read ZIP central record: %w", err)
		}
		if binary.LittleEndian.Uint32(h[:]) != zipCentralSignature {
			return nil, fmt.Errorf("%w: unexpected ZIP central record signature", ErrInvalidInput)
		}
		nameLength := uint64(binary.LittleEndian.Uint16(h[28:]))
		extraLength := uint64(binary.LittleEndian.Uint16(h[30:]))
		commentLength := uint64(binary.LittleEndian.Uint16(h[32:]))
		length := 46 + nameLength + extraLength + commentLength
		if length > endPosition-position {
			return nil, fmt.Errorf("%w: truncated ZIP name/extra/comment", ErrInvalidInput)
		}
		if binary.LittleEndian.Uint16(h[34:]) != 0 {
			return nil, fmt.Errorf("%w: split ZIP entry is unsupported", ErrInvalidInput)
		}
		if nameLength > uint64(b.limits.MaxPathBytes) {
			return nil, fmt.Errorf("%w: ZIP name exceeds MaxPathBytes=%d", ErrLimit, b.limits.MaxPathBytes)
		}
		if nameLength > uint64(b.limits.MaxTotalBytes-b.bytes) {
			return nil, fmt.Errorf("%w: ZIP names exceed MaxTotalBytes=%d", ErrLimit, b.limits.MaxTotalBytes)
		}
		name := make([]byte, int(nameLength))
		if err := readAt(ctx, r, name, int64(position+46)); err != nil {
			return nil, fmt.Errorf("read ZIP name: %w", err)
		}
		kind := "file"
		attributes := binary.LittleEndian.Uint32(h[38:])
		creator := h[5]
		mode := (attributes >> 16) & 0170000
		if strings.HasSuffix(string(name), "/") || attributes&0x10 != 0 || ((creator == 3 || creator == 19) && mode == 0040000) {
			kind = "directory"
		}
		if (creator == 3 || creator == 19) && mode == 0120000 {
			kind = "symlink"
		}
		if err := b.add(treeport.Entry{Path: string(name), Kind: kind}); err != nil {
			return nil, err
		}
		position += length
	}
	if uint64(len(b.entries)) != count {
		return nil, fmt.Errorf("%w: ZIP actual entry count %d differs from advertised %d", ErrInvalidInput, len(b.entries), count)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.entries, nil
}

func readAt(ctx context.Context, r io.ReaderAt, p []byte, offset int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := r.ReadAt(p, offset)
	if e := ctx.Err(); e != nil {
		return e
	}
	if n != len(p) && err == nil {
		return io.ErrUnexpectedEOF
	}
	return err
}
