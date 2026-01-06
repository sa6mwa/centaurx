package core

import "strings"

const defaultHistoryMax = 200

type historyBuffer struct {
	entries []string
	max     int
}

func newHistory(max int) *historyBuffer {
	if max <= 0 {
		max = defaultHistoryMax
	}
	return &historyBuffer{max: max}
}

func newHistoryFromPersisted(entries []string) *historyBuffer {
	h := newHistory(defaultHistoryMax)
	if len(entries) == 0 {
		return h
	}
	if len(entries) > h.max {
		entries = entries[len(entries)-h.max:]
	}
	h.entries = append([]string(nil), entries...)
	return h
}

func (h *historyBuffer) Append(entry string) bool {
	if h == nil {
		return false
	}
	if strings.TrimSpace(entry) == "" {
		return false
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == entry {
		return false
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > h.max {
		h.entries = h.entries[len(h.entries)-h.max:]
	}
	return true
}

func (h *historyBuffer) Entries() []string {
	if h == nil {
		return nil
	}
	return append([]string(nil), h.entries...)
}
