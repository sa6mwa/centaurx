package core

import (
	"context"
	"path/filepath"
	"testing"

	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/schema"
)

func TestActiveTabIsSessionScoped(t *testing.T) {
	repoRoot := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot}, ServiceDeps{
		RepoResolver: fakeRepoResolver{repo: repo},
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
	tabResp2, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create second tab: %v", err)
	}
	tabID2 := tabResp2.Tab.ID

	ctx1 := sessionprefs.WithContext(context.Background(), sessionprefs.New())
	ctx2 := sessionprefs.WithContext(context.Background(), sessionprefs.New())

	resp1, err := svc.ListTabs(ctx1, schema.ListTabsRequest{UserID: user})
	if err != nil {
		t.Fatalf("list tabs ctx1: %v", err)
	}
	if len(resp1.Tabs) == 0 {
		t.Fatalf("expected tabs in ctx1")
	}
	if resp1.ActiveTab != resp1.Tabs[0].ID {
		t.Fatalf("expected active tab %q, got %q", resp1.Tabs[0].ID, resp1.ActiveTab)
	}

	resp2, err := svc.ListTabs(ctx2, schema.ListTabsRequest{UserID: user})
	if err != nil {
		t.Fatalf("list tabs ctx2: %v", err)
	}
	if len(resp2.Tabs) == 0 {
		t.Fatalf("expected tabs in ctx2")
	}
	if resp2.ActiveTab != resp2.Tabs[0].ID {
		t.Fatalf("expected active tab %q in second session, got %q", resp2.Tabs[0].ID, resp2.ActiveTab)
	}

	if _, err := svc.ActivateTab(ctx1, schema.ActivateTabRequest{UserID: user, TabID: tabID2}); err != nil {
		t.Fatalf("activate tab: %v", err)
	}

	resp1, err = svc.ListTabs(ctx1, schema.ListTabsRequest{UserID: user})
	if err != nil {
		t.Fatalf("list tabs ctx1 after activate: %v", err)
	}
	if resp1.ActiveTab != tabID2 {
		t.Fatalf("expected active tab %q, got %q", tabID2, resp1.ActiveTab)
	}

	resp2, err = svc.ListTabs(ctx2, schema.ListTabsRequest{UserID: user})
	if err != nil {
		t.Fatalf("list tabs ctx2: %v", err)
	}
	if len(resp2.Tabs) == 0 {
		t.Fatalf("expected tabs in ctx2 after activate")
	}
	if resp2.ActiveTab != resp2.Tabs[0].ID {
		t.Fatalf("expected active tab %q in second session, got %q", resp2.Tabs[0].ID, resp2.ActiveTab)
	}

	if _, err := svc.CloseTab(ctx1, schema.CloseTabRequest{UserID: user, TabID: tabID2}); err != nil {
		t.Fatalf("close tab: %v", err)
	}
	resp1, err = svc.ListTabs(ctx1, schema.ListTabsRequest{UserID: user})
	if err != nil {
		t.Fatalf("list tabs ctx1 after close: %v", err)
	}
	if len(resp1.Tabs) == 0 {
		t.Fatalf("expected tabs in ctx1 after close")
	}
	if resp1.ActiveTab != resp1.Tabs[0].ID {
		t.Fatalf("expected active tab %q after close, got %q", resp1.Tabs[0].ID, resp1.ActiveTab)
	}
}
