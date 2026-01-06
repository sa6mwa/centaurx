package core

import "pkt.systems/centaurx/schema"

// bufferView is a snapshot of a buffer's visible state.
type bufferView struct {
	Lines        []string
	TotalLines   int
	ScrollOffset int
	AtBottom     bool
}

const defaultMaxLines = schema.DefaultBufferMaxLines

// buffer stores scrollback lines and scroll state.
// ScrollOffset is the number of lines from the bottom; 0 means at bottom.
type buffer struct {
	lines        []string
	scrollOffset int
	maxLines     int
}

// persistedBuffer captures buffer lines and scroll offset for persistence.
type persistedBuffer struct {
	Lines        []string
	ScrollOffset int
}

// Append adds lines to the buffer. If the buffer is scrolled up, the scroll offset
// is increased to keep the view anchored.
func (b *buffer) Append(lines ...string) {
	if len(lines) == 0 {
		return
	}
	b.lines = append(b.lines, lines...)
	if b.scrollOffset > 0 {
		b.scrollOffset += len(lines)
	}
	maxLines := b.maxLines
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	if maxLines > 0 && len(b.lines) > maxLines {
		trim := len(b.lines) - maxLines
		b.lines = b.lines[trim:]
		if b.scrollOffset > len(b.lines) {
			b.scrollOffset = len(b.lines)
		}
		if b.scrollOffset < 0 {
			b.scrollOffset = 0
		}
	}
}

// ResetScroll returns the view to the bottom.
func (b *buffer) ResetScroll() {
	b.scrollOffset = 0
}

// Scroll adjusts the scroll offset by delta. Positive delta scrolls up (older lines),
// negative delta scrolls down. Limit is the viewport height.
func (b *buffer) Scroll(delta, limit int) {
	b.scrollOffset = clampScroll(b.scrollOffset+delta, len(b.lines), limit)
}

// Snapshot returns a view of the buffer for the given viewport limit.
func (b *buffer) Snapshot(limit int) bufferView {
	total := len(b.lines)
	if limit <= 0 || limit > total {
		limit = total
	}

	maxScroll := maxScroll(total, limit)
	if b.scrollOffset > maxScroll {
		b.scrollOffset = maxScroll
	}

	end := total - b.scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}

	lines := make([]string, end-start)
	copy(lines, b.lines[start:end])

	return bufferView{
		Lines:        lines,
		TotalLines:   total,
		ScrollOffset: b.scrollOffset,
		AtBottom:     b.scrollOffset == 0,
	}
}

// Export returns the buffer state for persistence.
func (b *buffer) Export() persistedBuffer {
	if b == nil {
		return persistedBuffer{}
	}
	lines := append([]string(nil), b.lines...)
	offset := b.scrollOffset
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	return persistedBuffer{
		Lines:        lines,
		ScrollOffset: offset,
	}
}

// newBuffer returns a buffer with default limits applied.
func newBuffer() *buffer {
	return &buffer{maxLines: defaultMaxLines}
}

func newBufferWithMaxLines(maxLines int) *buffer {
	buf := newBuffer()
	if maxLines > 0 {
		buf.maxLines = maxLines
	}
	return buf
}

// newBufferFromPersisted constructs a buffer from persisted data.
func newBufferFromPersistedWithMaxLines(state persistedBuffer, maxLines int) *buffer {
	b := newBufferWithMaxLines(maxLines)
	lines := append([]string(nil), state.Lines...)
	if b.maxLines > 0 && len(lines) > b.maxLines {
		lines = lines[len(lines)-b.maxLines:]
	}
	offset := state.ScrollOffset
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	b.lines = lines
	b.scrollOffset = offset
	return b
}

func maxScroll(total, limit int) int {
	if total <= 0 || limit <= 0 {
		return 0
	}
	if total <= limit {
		return 0
	}
	return total - limit
}

func clampScroll(offset, total, limit int) int {
	max := maxScroll(total, limit)
	if offset < 0 {
		return 0
	}
	if offset > max {
		return max
	}
	return offset
}
