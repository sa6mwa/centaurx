package runnerconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndEnv(t *testing.T) {
	t.Setenv("SOCK_PATH", "/tmp/runner.sock")
	path := writeRunnerConfig(t, `
socket_path: $SOCK_PATH
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SocketPath != "/tmp/runner.sock" {
		t.Fatalf("expected expanded socket path, got %q", cfg.SocketPath)
	}
	if cfg.Binary != "codex" {
		t.Fatalf("expected default binary, got %q", cfg.Binary)
	}
	if cfg.KeepaliveIntervalSeconds != 10 || cfg.KeepaliveMisses != 3 {
		t.Fatalf("expected default keepalive values, got %d/%d", cfg.KeepaliveIntervalSeconds, cfg.KeepaliveMisses)
	}
}

func TestLoadRejectsMissingPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatalf("expected error for empty config path")
	}
	path := writeRunnerConfig(t, `
binary: codex
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "socket_path") {
		t.Fatalf("expected socket_path error, got %v", err)
	}
}

func writeRunnerConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runner.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
