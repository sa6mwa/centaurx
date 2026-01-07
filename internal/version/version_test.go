package version

import (
	"runtime/debug"
	"strings"
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
	got := pseudoFromBuildInfo(info, false)
	if got == "" {
		t.Fatalf("expected pseudo version")
	}
	if wantPrefix := "v0.0.0-20250102030405-1234567890ab"; got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("unexpected version prefix: %q", got)
	}
	if strings.Contains(got, "+dirty") {
		t.Fatalf("expected no dirty suffix, got %q", got)
	}
	gotDirty := pseudoFromBuildInfo(info, true)
	if !strings.Contains(gotDirty, "+dirty") {
		t.Fatalf("expected dirty suffix, got %q", gotDirty)
	}
	if got := normalizeVersion("v1.2.3+dirty", false); got != "v1.2.3" {
		t.Fatalf("expected dirty suffix removed, got %q", got)
	}
	if got := normalizeVersion("v1.2.3+dirty", true); got != "v1.2.3+dirty" {
		t.Fatalf("expected dirty suffix preserved, got %q", got)
	}
	if pseudoFromBuildInfo(nil, false) != "" {
		t.Fatalf("expected empty version for nil build info")
	}
}
