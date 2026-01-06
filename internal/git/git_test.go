package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if _, err := Run(context.Background(), dir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := Run(context.Background(), dir, "status"); err != nil {
		t.Fatalf("git status: %v", err)
	}
}

func TestRunOutsideRepoErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if _, err := Run(context.Background(), dir, "status"); err == nil {
		t.Fatalf("expected error outside repo")
	}
}

func TestAddAllAndCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if _, err := Run(context.Background(), dir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := Run(context.Background(), dir, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if _, err := Run(context.Background(), dir, "config", "user.name", "tester"); err != nil {
		t.Fatalf("git config name: %v", err)
	}
	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := AddAll(context.Background(), dir); err != nil {
		t.Fatalf("git add: %v", err)
	}
	out, err := Commit(context.Background(), dir, "init")
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if !strings.Contains(out, "1 file") {
		t.Fatalf("unexpected commit output: %q", out)
	}
}
