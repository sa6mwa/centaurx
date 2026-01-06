package format

import (
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestFormatCommandDedupesLeadingCommandLine(t *testing.T) {
	item := &schema.ItemEvent{
		Type:             schema.ItemCommandExecution,
		Command:          "ls -la",
		AggregatedOutput: "$ ls -la\nfile1\nfile2\n",
	}
	lines := formatCommand(item, schema.EventItemCompleted)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d (%v)", len(lines), lines)
	}
	if lines[0] != "$ ls -la" {
		t.Fatalf("expected command line first, got %q", lines[0])
	}
	if lines[1] != "file1" {
		t.Fatalf("expected first output line, got %q", lines[1])
	}
}

func TestFormatCommandKeepsDistinctOutput(t *testing.T) {
	item := &schema.ItemEvent{
		Type:             schema.ItemCommandExecution,
		Command:          "ls -la",
		AggregatedOutput: "total 10\nfile1\n",
	}
	lines := formatCommand(item, schema.EventItemCompleted)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d (%v)", len(lines), lines)
	}
	if lines[0] != "$ ls -la" {
		t.Fatalf("expected command line first, got %q", lines[0])
	}
	if lines[1] != "total 10" {
		t.Fatalf("expected output line, got %q", lines[1])
	}
}
