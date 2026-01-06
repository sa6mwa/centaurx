package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnsupportedConfigVersion(t *testing.T) {
	path := writeConfig(t, `
config_version: 3
runner:
  runtime: podman
  image: demo
  sock_dir: /socks
  repo_root: /repos
  podman:
    address: unix:///run/user/1000/podman/podman.sock
ssh:
  key_store_path: /state/ssh/keys.bundle
  key_dir: /state/ssh/keys
  agent_dir: /state/ssh/agent
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported config_version") {
		t.Fatalf("expected config_version error, got %v", err)
	}
}

func TestLoadRejectsUnsupportedRuntime(t *testing.T) {
	path := writeConfig(t, `
config_version: 4
runner:
  runtime: nope
  image: demo
  sock_dir: /socks
  repo_root: /repos
ssh:
  key_store_path: /state/ssh/keys.bundle
  key_dir: /state/ssh/keys
  agent_dir: /state/ssh/agent
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported runner.runtime") {
		t.Fatalf("expected runtime error, got %v", err)
	}
}

func TestLoadRejectsInvalidHTTPBaseURL(t *testing.T) {
	path := writeConfig(t, `
config_version: 4
runner:
  runtime: podman
  image: demo
  sock_dir: /socks
  repo_root: /repos
  podman:
    address: unix:///run/user/1000/podman/podman.sock
ssh:
  key_store_path: /state/ssh/keys.bundle
  key_dir: /state/ssh/keys
  agent_dir: /state/ssh/agent
http:
  base_url: example.com
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "http.base_url") {
		t.Fatalf("expected base_url error, got %v", err)
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO", "bar")
	value := expandEnv("$FOO/$UID/$GID/$MISSING")
	if !strings.HasPrefix(value, "bar/") {
		t.Fatalf("expected env expansion, got %q", value)
	}
	if strings.Contains(value, "$UID") || strings.Contains(value, "$GID") {
		t.Fatalf("expected UID/GID expansion, got %q", value)
	}
	if !strings.HasSuffix(value, "/$MISSING") {
		t.Fatalf("expected missing vars to remain, got %q", value)
	}
}

func TestWriteDefaultRespectsOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	written, err := WriteDefault(path, false)
	if err != nil {
		t.Fatalf("write default: %v", err)
	}
	if written != path {
		t.Fatalf("expected path %q, got %q", path, written)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config to exist: %v", err)
	}
	if _, err := WriteDefault(path, false); err == nil {
		t.Fatalf("expected error when config exists")
	}
	if _, err := WriteDefault(path, true); err != nil {
		t.Fatalf("expected overwrite to succeed: %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
