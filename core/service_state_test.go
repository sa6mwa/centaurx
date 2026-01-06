package core

import (
	"context"
	"path/filepath"
	"testing"

	"pkt.systems/centaurx/internal/persist"
	"pkt.systems/centaurx/schema"
)

func TestPersistedRepoPathIsEmpty(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}

	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RepoResolver: resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	user := schema.UserID("alice")
	if _, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	}); err != nil {
		t.Fatalf("create tab: %v", err)
	}

	store, err := persist.NewStore(stateDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	snapshot, ok, err := store.Load(user)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !ok || len(snapshot.Tabs) == 0 {
		t.Fatalf("expected persisted tab snapshot")
	}
	if snapshot.Tabs[0].Repo.Name != repo.Name {
		t.Fatalf("expected repo name %q, got %q", repo.Name, snapshot.Tabs[0].Repo.Name)
	}
	if snapshot.Tabs[0].Repo.Path != "" {
		t.Fatalf("expected empty repo path, got %q", snapshot.Tabs[0].Repo.Path)
	}
}
