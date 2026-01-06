package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiredConfigMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	_, _, err := loadRequiredConfig(path)
	if err == nil {
		t.Fatalf("expected missing config error")
	}
	if !strings.Contains(err.Error(), "centaurx bootstrap") {
		t.Fatalf("expected bootstrap hint, got %v", err)
	}
}

func TestResolveCentaurxBinaryExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "centaurx")
	if err := os.WriteFile(path, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write temp bin: %v", err)
	}
	got, err := resolveCentaurxBinary(path)
	if err != nil {
		t.Fatalf("resolve centaurx binary: %v", err)
	}
	if got != path {
		t.Fatalf("resolveCentaurxBinary = %q, want %q", got, path)
	}
}

func TestResolveOutputPathDefaultsToConfigDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	got, err := resolveOutputPath(configPath, "", "pktsystems-centaurx.oci.tar")
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	want := filepath.Join(filepath.Dir(configPath), "containers", "pktsystems-centaurx.oci.tar")
	if got != want {
		t.Fatalf("resolveOutputPath = %q, want %q", got, want)
	}
}

func TestResolveOutputPathOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	override := filepath.Join(t.TempDir(), "custom.oci.tar")
	got, err := resolveOutputPath(configPath, override, "ignored.oci.tar")
	if err != nil {
		t.Fatalf("resolveOutputPath override: %v", err)
	}
	if got != override {
		t.Fatalf("resolveOutputPath override = %q, want %q", got, override)
	}
}

func TestStripImageTag(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{name: "tagged", image: "docker.io/pktsystems/centaurx:latest", want: "docker.io/pktsystems/centaurx"},
		{name: "port", image: "registry:5000/repo:tag", want: "registry:5000/repo"},
		{name: "digest", image: "repo@sha256:deadbeef", want: "repo"},
		{name: "untagged", image: "pktsystems/centaurx", want: "pktsystems/centaurx"},
	}
	for _, tc := range tests {
		if got := stripImageTag(tc.image); got != tc.want {
			t.Fatalf("%s: stripImageTag(%q) = %q, want %q", tc.name, tc.image, got, tc.want)
		}
	}
}
