//go:build linux

package terminal

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// All methods are called under Engine.mu. Bytes split across reads remain in
// pending until a complete UTF-8 rune (or EOF) is available.
type outputBuffer struct {
	data, pending []byte
	omitted       int64
	limit         int
}

func (b *outputBuffer) append(p []byte, final bool) {
	b.pending = append(b.pending, p...)
	for len(b.pending) > 0 {
		if !final && !utf8.FullRune(b.pending) {
			break
		}
		_, n := utf8.DecodeRune(b.pending)
		// Keep source bytes until delivery so replacement characters do not
		// inflate the number of omitted raw bytes.
		b.data = append(b.data, b.pending[:n]...)
		b.pending = b.pending[n:]
	}
	if len(b.data) > b.limit {
		drop := len(b.data) - b.limit
		for start := max(0, drop-3); start < drop; start++ {
			_, n := utf8.DecodeRune(b.data[start:])
			if n > 1 && start+n > drop {
				drop = start + n
				break
			}
		}
		b.omitted += int64(drop)
		b.data = bytes.Clone(b.data[drop:])
	}
}
func boundaryPrefix(p []byte, budget int) int {
	used, offset := 0, 0
	for offset < len(p) {
		r, n := utf8.DecodeRune(p[offset:])
		size := utf8.RuneLen(r)
		if used+size > budget {
			break
		}
		used += size
		offset += n
	}
	return offset
}
func boundarySuffix(p []byte, budget int) int {
	used, offset := 0, len(p)
	for offset > 0 {
		r, n := utf8.DecodeLastRune(p[:offset])
		size := utf8.RuneLen(r)
		if used+size > budget {
			break
		}
		used += size
		offset -= n
	}
	return offset
}
func (b *outputBuffer) take(budget int) (string, bool, int64) {
	p := b.data
	omitted := b.omitted
	visible := strings.ToValidUTF8(string(p), "\ufffd")
	if len(visible) <= budget {
		out := visible
		b.data = nil
		b.omitted = 0
		return out, omitted > 0, omitted
	}
	head := boundaryPrefix(p, budget/2)
	tail := boundarySuffix(p, budget-budget/2)
	if budget < 16 {
		head = boundaryPrefix(p, budget)
		tail = len(p)
	}
	omitted += int64(tail - head)
	out := strings.ToValidUTF8(string(p[:head]), "\ufffd") + "\n[output truncated]\n" + strings.ToValidUTF8(string(p[tail:]), "\ufffd")
	b.data = nil
	b.omitted = 0
	return out, true, omitted
}
