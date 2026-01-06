package core

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestServicePerUserRepoIsolation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	stateDir := t.TempDir()

	svc, err := NewService(schema.ServiceConfig{
		RepoRoot: repoRoot,
		StateDir: stateDir,
	}, ServiceDeps{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	alice := schema.UserID("alice")
	bob := schema.UserID("bob")

	alpha := schema.RepoName("alpha")
	resp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     alice,
		RepoName:   alpha,
		CreateRepo: true,
	})
	if err != nil {
		t.Fatalf("create repo for alice: %v", err)
	}
	wantAlicePath := filepath.Join(repoRoot, "alice", "alpha")
	if resp.Tab.Repo.Path != wantAlicePath {
		t.Fatalf("alice repo path mismatch: got %q want %q", resp.Tab.Repo.Path, wantAlicePath)
	}

	_, err = svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     bob,
		RepoName:   alpha,
		CreateRepo: false,
	})
	if err == nil {
		t.Fatalf("expected bob to fail opening alice repo")
	}
	if err != schema.ErrRepoNotFound {
		t.Fatalf("expected repo not found for bob, got %v", err)
	}

	_, err = svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     bob,
		RepoName:   alpha,
		CreateRepo: true,
	})
	if err != nil {
		t.Fatalf("create repo for bob: %v", err)
	}
	wantBobPath := filepath.Join(repoRoot, "bob", "alpha")
	if wantBobPath == wantAlicePath {
		t.Fatalf("expected different paths for alice and bob")
	}

	listAlice, err := svc.ListRepos(context.Background(), schema.ListReposRequest{UserID: alice})
	if err != nil {
		t.Fatalf("list repos for alice: %v", err)
	}
	if len(listAlice.Repos) != 1 || listAlice.Repos[0].Name != alpha {
		t.Fatalf("alice repos mismatch: %+v", listAlice.Repos)
	}

	listBob, err := svc.ListRepos(context.Background(), schema.ListReposRequest{UserID: bob})
	if err != nil {
		t.Fatalf("list repos for bob: %v", err)
	}
	if len(listBob.Repos) != 1 || listBob.Repos[0].Name != alpha {
		t.Fatalf("bob repos mismatch: %+v", listBob.Repos)
	}
}

func TestServiceRejectsInvalidUserID(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()

	svc, err := NewService(schema.ServiceConfig{
		RepoRoot: repoRoot,
		StateDir: stateDir,
	}, ServiceDeps{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = svc.ListRepos(context.Background(), schema.ListReposRequest{UserID: "Alice"})
	if err == nil {
		t.Fatalf("expected invalid user error")
	}
}
