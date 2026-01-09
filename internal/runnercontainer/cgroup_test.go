package runnercontainer

import (
	"strings"
	"testing"
)

func TestResolveCgroupParentPathWithCurrentRelative(t *testing.T) {
	current := "/user.slice/user-1000.slice/session-2.scope"
	parent, err := resolveCgroupParentPathWithCurrent(current, "centaurx-runner")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "/user.slice/user-1000.slice/session-2.scope/centaurx-runner"
	if parent != want {
		t.Fatalf("expected %q, got %q", want, parent)
	}
}

func TestResolveCgroupParentPathWithCurrentAbsolute(t *testing.T) {
	current := "/user.slice/user-1000.slice/session-2.scope"
	parent, err := resolveCgroupParentPathWithCurrent(current, "/custom.slice/centaurx")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "/custom.slice/centaurx"
	if parent != want {
		t.Fatalf("expected %q, got %q", want, parent)
	}
}

func TestCpuMaxFromPercent(t *testing.T) {
	value, ok := cpuMaxFromPercent(8, 70)
	if !ok {
		t.Fatalf("expected cpu max")
	}
	want := "560000 100000"
	if value != want {
		t.Fatalf("expected %q, got %q", want, value)
	}
}

func TestCpuMaxFromPercentClamps(t *testing.T) {
	value, ok := cpuMaxFromPercent(4, 150)
	if !ok {
		t.Fatalf("expected cpu max")
	}
	want := "400000 100000"
	if value != want {
		t.Fatalf("expected %q, got %q", want, value)
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
	value, ok := memoryMaxFromPercentWithTotal(10*1024*1024*1024, 70)
	if !ok {
		t.Fatalf("expected memory max")
	}
	want := "7516192768"
	if value != want {
		t.Fatalf("expected %q, got %q", want, value)
	}
}
