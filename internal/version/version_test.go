package version

import (
	"runtime/debug"
	"testing"
	"time"
)

func TestCurrentPrefersBuildVersion(t *testing.T) {
	old := buildVersion
	buildVersion = "v1.2.3"
	t.Cleanup(func() { buildVersion = old })

	if got := Current(); got != "v1.2.3" {
		t.Fatalf("expected build version, got %q", got)
	}
}

func TestPseudoFromBuildInfo(t *testing.T) {
	ts := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	info := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "1234567890abcdef"},
			{Key: "vcs.time", Value: ts.Format(time.RFC3339)},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := pseudoFromBuildInfo(info)
	if got == "" {
		t.Fatalf("expected pseudo version")
	}
	if wantPrefix := "v0.0.0-20250102030405-1234567890ab"; got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("unexpected version prefix: %q", got)
	}
	if got[len(got)-6:] != "+dirty" {
		t.Fatalf("expected dirty suffix, got %q", got)
	}
	if pseudoFromBuildInfo(nil) != "" {
		t.Fatalf("expected empty version for nil build info")
	}
}
