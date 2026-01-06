package userhome

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureHomeRendersTemplates(t *testing.T) {
	temp := t.TempDir()
	skelDir := filepath.Join(temp, "skel")
	if err := os.MkdirAll(filepath.Join(skelDir, ".codex"), 0o700); err != nil {
		t.Fatalf("skel mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skelDir, ".codex", "config.toml.tmpl"), []byte("model = \"{{ .Codex.Model }}\"\n"), 0o600); err != nil {
		t.Fatalf("skel write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skelDir, "plain.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("skel write: %v", err)
	}

	stateDir := filepath.Join(temp, "state")
	data := TemplateData{Codex: CodexConfig{Model: "gpt-test"}}
	home, err := EnsureHome(stateDir, "alice", skelDir, data)
	if err != nil {
		t.Fatalf("ensure home: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "plain.txt")); err != nil {
		t.Fatalf("plain file: %v", err)
	}

	cfgPath := filepath.Join(home, ".codex", "config.toml")
	cfgRaw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config.toml: %v", err)
	}
	if !strings.Contains(string(cfgRaw), "gpt-test") {
		t.Fatalf("expected rendered model, got %q", string(cfgRaw))
	}

	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml.tmpl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected template to be removed, got %v", err)
	}
}
