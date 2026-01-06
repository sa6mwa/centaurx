package main

import (
	"strings"
	"testing"
)

func TestParseMockArgsResumeOrdering(t *testing.T) {
	cfg, err := parseMockArgs([]string{"exec", "--json", "resume", "session-1", "-"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.jsonOutput {
		t.Fatalf("expected json output enabled")
	}
	if cfg.resumeID != "session-1" {
		t.Fatalf("expected resume id session-1, got %q", cfg.resumeID)
	}

	_, err = parseMockArgs([]string{"exec", "resume", "session-1", "--json", "-"})
	if err == nil {
		t.Fatalf("expected error for flag after resume")
	}
	if !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}
