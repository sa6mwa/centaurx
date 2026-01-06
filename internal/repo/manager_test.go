package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerCreateAndResolveRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	manager, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	repoRef, err := manager.CreateRepo("alpha")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repoRef.Path == "" {
		t.Fatalf("expected repo path")
	}
	if _, err := os.Stat(filepath.Join(repoRef.Path, ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}

	branchCmd := exec.Command("git", "-C", repoRef.Path, "symbolic-ref", "--short", "HEAD")
	out, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	if strings.TrimSpace(string(out)) != "centaurx" {
		t.Fatalf("expected branch centaurx, got %q", strings.TrimSpace(string(out)))
	}

	resolved, err := manager.ResolveRepo("alpha")
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if resolved.Path != repoRef.Path {
		t.Fatalf("resolved path mismatch: %q vs %q", resolved.Path, repoRef.Path)
	}
}

func TestManagerListReposFiltersNonGitDirs(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	withGit := filepath.Join(root, "alpha")
	if err := os.MkdirAll(filepath.Join(withGit, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	repos, err := manager.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Name != "alpha" {
		t.Fatalf("expected repo alpha, got %q", repos[0].Name)
	}
}
