package core

import (
	"context"
	"path/filepath"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestHistoryPersistsAndDedupes(t *testing.T) {
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
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	tabID := tabResp.Tab.ID

	if _, err := svc.AppendHistory(context.Background(), schema.AppendHistoryRequest{
		UserID: user,
		TabID:  tabID,
		Entry:  "first",
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	if _, err := svc.AppendHistory(context.Background(), schema.AppendHistoryRequest{
		UserID: user,
		TabID:  tabID,
		Entry:  "first",
	}); err != nil {
		t.Fatalf("append history duplicate: %v", err)
	}
	if _, err := svc.AppendHistory(context.Background(), schema.AppendHistoryRequest{
		UserID: user,
		TabID:  tabID,
		Entry:  "second",
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	hist, err := svc.GetHistory(context.Background(), schema.GetHistoryRequest{
		UserID: user,
		TabID:  tabID,
	})
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(hist.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(hist.Entries))
	}

	svc2, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RepoResolver: resolver,
	})
	if err != nil {
		t.Fatalf("new service reload: %v", err)
	}
	hist2, err := svc2.GetHistory(context.Background(), schema.GetHistoryRequest{
		UserID: user,
		TabID:  tabID,
	})
	if err != nil {
		t.Fatalf("get history reload: %v", err)
	}
	if len(hist2.Entries) != 2 || hist2.Entries[0] != "first" || hist2.Entries[1] != "second" {
		t.Fatalf("unexpected history after reload: %v", hist2.Entries)
	}
}
