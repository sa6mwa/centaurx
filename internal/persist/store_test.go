package persist

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestStoreLoadMissing(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	_, ok, err := store.Load("alice")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ok {
		t.Fatalf("expected missing snapshot")
	}
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	snapshot := UserSnapshot{
		Order: []schema.TabID{"tab1"},
		Tabs: []TabSnapshot{
			{
				ID:        "tab1",
				Name:      "demo",
				Repo:      schema.RepoRef{Name: "demo"},
				Model:     "gpt-5.2-codex",
				SessionID: "sess-1",
				Buffer: BufferSnapshot{
					Lines:        []string{"hi"},
					ScrollOffset: 0,
				},
				History: []string{"cmd"},
			},
		},
		System: BufferSnapshot{
			Lines:        []string{"system"},
			ScrollOffset: 1,
		},
		Theme: "outrun",
	}
	if err := store.Save("alice", snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := store.Load("alice")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("expected snapshot to exist")
	}
	if !reflect.DeepEqual(snapshot, got) {
		t.Fatalf("snapshot mismatch:\nwant: %+v\ngot:  %+v", snapshot, got)
	}
}

func TestStoreLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	path := filepath.Join(dir, "alice.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	if _, _, err := store.Load("alice"); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}
