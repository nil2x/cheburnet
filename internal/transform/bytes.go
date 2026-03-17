package transform

import (
	"bytes"
	"fmt"
)

// Example:
//
//	{8, 16, 255} -> "08 10 ff"
func BytesToHex(b []byte) string {
	return fmt.Sprintf("% x", b)
}

// BytesToChunks splits slice into sub-slices.
// Returns copies of the original slice, not references to it.
//
// size specifies maximum number of elements in each sub-slice.
// Length of last sub-slice may be less or equal to the size.
// Zero size turns b into sub-slice.
//
// count specifies maximum number of sub-slices after splitting by size.
// Length of last sub-slice may be less, equal or more of the size.
// Zero count skips this logic.
//
// Example:
//
//	BytesToChunks([]byte{1, 2, 3, 4, 5}, 2, 0) => [][]byte{{1, 2}, {3, 4}, {5}}
//	BytesToChunks([]byte{1, 2, 3, 4, 5}, 2, 2) => [][]byte{{1, 2}, {3, 4, 5}}
func BytesToChunks(b []byte, size int, count int) [][]byte {
	b = bytes.Clone(b)

	if size == 0 || count == 1 {
		return [][]byte{b}
	}

	chunks := [][]byte{}

	for start := 0; start < len(b); start += size {
		end := min(start+size, len(b))
		chunk := b[start:end]
		chunks = append(chunks, chunk)
	}

	if count > 0 && len(chunks) > count-1 {
		counted := chunks[:count-1]
		remaining := chunks[count-1:]
		merged := []byte{}

		for _, b := range remaining {
			merged = append(merged, b...)
		}

		counted = append(counted, merged)

		return counted
	}

	return chunks
}
