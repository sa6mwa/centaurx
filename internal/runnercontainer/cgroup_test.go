package runnercontainer

import (
	"strings"
	"testing"
)

func TestCpuMaxFromPercent(t *testing.T) {
	value, ok := nanoCPUsFromPercent(8, 70)
	if !ok {
		t.Fatalf("expected nano cpus")
	}
	want := int64(5600000000)
	if value != want {
		t.Fatalf("expected %d, got %d", want, value)
	}
}

func TestCpuMaxFromPercentClamps(t *testing.T) {
	value, ok := nanoCPUsFromPercent(4, 150)
	if !ok {
		t.Fatalf("expected nano cpus")
	}
	want := int64(4000000000)
	if value != want {
		t.Fatalf("expected %d, got %d", want, value)
	}
}

func TestParseMemTotalBytes(t *testing.T) {
	raw := strings.NewReader("MemTotal:       8192000 kB\n")
	value, err := parseMemTotalBytes(raw)
	if err != nil {
		t.Fatalf("parse meminfo: %v", err)
	}
	if value != 8192000*1024 {
		t.Fatalf("expected %d, got %d", 8192000*1024, value)
	}
}

func TestMemoryMaxFromPercentWithTotal(t *testing.T) {
	value, ok := memoryBytesFromPercentWithTotal(10*1024*1024*1024, 70)
	if !ok {
		t.Fatalf("expected memory bytes")
	}
	want := int64(7516192768)
	if value != want {
		t.Fatalf("expected %d, got %d", want, value)
	}
}
