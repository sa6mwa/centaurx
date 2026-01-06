package core

import "testing"

func TestBufferScrollAnchorsOnAppend(t *testing.T) {
	b := &buffer{maxLines: 100}
	b.Append("one", "two", "three", "four", "five")
	b.Scroll(2, 3) // scroll up two lines with viewport size 3
	if b.scrollOffset != 2 {
		t.Fatalf("expected scroll offset 2, got %d", b.scrollOffset)
	}
	b.Append("six", "seven")
	if b.scrollOffset != 4 {
		t.Fatalf("expected scroll offset 4 after append, got %d", b.scrollOffset)
	}
	view := b.Snapshot(3)
	if view.AtBottom {
		t.Fatalf("expected not at bottom after scroll")
	}
	if len(view.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(view.Lines))
	}
}

func TestBufferRespectsMaxLines(t *testing.T) {
	b := &buffer{maxLines: 3}
	b.Append("one", "two", "three", "four", "five")
	view := b.Snapshot(10)
	if view.TotalLines != 3 {
		t.Fatalf("expected total lines 3, got %d", view.TotalLines)
	}
	if len(view.Lines) != 3 {
		t.Fatalf("expected 3 visible lines, got %d", len(view.Lines))
	}
	if view.Lines[0] != "three" || view.Lines[2] != "five" {
		t.Fatalf("unexpected lines: %+v", view.Lines)
	}
}

func TestBufferResetScroll(t *testing.T) {
	b := &buffer{maxLines: 10}
	b.Append("one", "two", "three")
	b.Scroll(1, 2)
	if b.scrollOffset == 0 {
		t.Fatalf("expected scroll offset > 0")
	}
	b.ResetScroll()
	if b.scrollOffset != 0 {
		t.Fatalf("expected scroll offset 0, got %d", b.scrollOffset)
	}
}

func TestBufferScrollClampsToBounds(t *testing.T) {
	b := &buffer{maxLines: 10}
	b.Append("one", "two", "three", "four", "five")

	b.Scroll(10, 3)
	if b.scrollOffset != 2 {
		t.Fatalf("expected scroll offset 2, got %d", b.scrollOffset)
	}

	b.Scroll(-10, 3)
	if b.scrollOffset != 0 {
		t.Fatalf("expected scroll offset 0, got %d", b.scrollOffset)
	}
}

func TestBufferSnapshotClampsOffset(t *testing.T) {
	b := &buffer{maxLines: 10}
	b.Append("one", "two", "three", "four", "five")
	b.scrollOffset = 10

	view := b.Snapshot(3)
	if view.ScrollOffset != 2 {
		t.Fatalf("expected scroll offset 2, got %d", view.ScrollOffset)
	}
	if len(view.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(view.Lines))
	}
	if view.Lines[0] != "one" || view.Lines[2] != "three" {
		t.Fatalf("unexpected lines: %v", view.Lines)
	}
}
