package core

import (
	"path/filepath"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestRepoPath(t *testing.T) {
	path, err := RepoPath("/repos", "alice", "demo")
	if err != nil {
		t.Fatalf("repo path: %v", err)
	}
	want := filepath.Join("/repos", "alice", "demo")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestRepoPathRejectsEmptyInputs(t *testing.T) {
	if _, err := RepoPath("", "alice", "demo"); err == nil {
		t.Fatalf("expected error for empty repo root")
	}
	if _, err := RepoPath("/repos", "", "demo"); err == nil {
		t.Fatalf("expected error for empty user")
	}
	if _, err := RepoPath("/repos", "alice", "demo/sub"); err == nil {
		t.Fatalf("expected error for invalid repo name")
	}
}

func TestRepoRefForUser(t *testing.T) {
	ref := RepoRefForUser("/repos", "alice", "demo")
	if ref.Name != "demo" {
		t.Fatalf("expected repo name demo, got %q", ref.Name)
	}
	if ref.Path == "" {
		t.Fatalf("expected repo path to be populated")
	}
	ref = RepoRefForUser("", "alice", "demo")
	if ref.Path != "" {
		t.Fatalf("expected empty path for invalid repo root, got %q", ref.Path)
	}
}

func TestMapRepoPath(t *testing.T) {
	root := filepath.Join("/repos", "alice")
	path := filepath.Join(root, "demo")
	if got, err := MapRepoPath(root, "", path); err != nil || got != path {
		t.Fatalf("expected passthrough mapping, got %q (%v)", got, err)
	}
	if got, err := MapRepoPath(root, root, path); err != nil || got != path {
		t.Fatalf("expected passthrough mapping, got %q (%v)", got, err)
	}
	mapped, err := MapRepoPath(root, "/container/repos", path)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	want := filepath.Join("/container/repos", "demo")
	if mapped != want {
		t.Fatalf("expected %q, got %q", want, mapped)
	}
	if _, err := MapRepoPath(root, "/container/repos", "/other/demo"); err != schema.ErrInvalidRepo {
		t.Fatalf("expected invalid repo error, got %v", err)
	}
	if mapped, err := MapRepoPath(root, "/container/repos", root); err != nil || mapped != "/container/repos" {
		t.Fatalf("expected root mapping, got %q (%v)", mapped, err)
	}
}
