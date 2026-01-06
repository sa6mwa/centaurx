package sshserver

import (
	"strings"
	"testing"
)

func TestReadKeysShiftTab(t *testing.T) {
	keys := make(chan key, 1)
	go readKeys(strings.NewReader("\x1b[Z"), keys)
	k, ok := <-keys
	if !ok {
		t.Fatalf("expected key, got closed channel")
	}
	if k.kind != keyShiftTab {
		t.Fatalf("expected shift tab, got %v", k.kind)
	}
}
