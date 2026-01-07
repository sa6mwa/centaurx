package httpapi

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type sessionTestKey struct{}

func TestSessionStoreCreateGetDelete(t *testing.T) {
	store := newSessionStore(time.Hour, "")
	token, sess := store.create("alice")
	if token == "" {
		t.Fatalf("expected token")
	}
	if sess.userID != "alice" {
		t.Fatalf("unexpected user id: %q", sess.userID)
	}
	if sess.ctx == nil || sess.prefs == nil {
		t.Fatalf("expected session context and prefs")
	}
	if _, ok := store.get(token); !ok {
		t.Fatalf("expected session to be found")
	}
	store.delete(token)
	if _, ok := store.get(token); ok {
		t.Fatalf("expected session to be deleted")
	}
	select {
	case <-sess.ctx.Done():
	default:
		t.Fatalf("expected session context to be canceled")
	}
}

func TestSessionStoreExpiration(t *testing.T) {
	store := newSessionStore(5*time.Millisecond, "")
	token, sess := store.create("alice")
	time.Sleep(10 * time.Millisecond)
	if _, ok := store.get(token); ok {
		t.Fatalf("expected expired session")
	}
	select {
	case <-sess.ctx.Done():
	default:
		t.Fatalf("expected session context to be canceled")
	}
}

func TestSessionStoreBaseContext(t *testing.T) {
	store := newSessionStore(time.Hour, "")
	baseKey := sessionTestKey{}
	base := context.WithValue(context.Background(), baseKey, "value")
	store.setBaseContext(base)
	_, sess := store.create("alice")
	if got := sess.ctx.Value(baseKey); got != "value" {
		t.Fatalf("expected base context value, got %v", got)
	}
}

func TestSessionStorePersistsSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	store := newSessionStore(time.Hour, path)
	token, _ := store.create("alice")

	loaded := newSessionStore(time.Hour, path)
	if _, ok := loaded.get(token); !ok {
		t.Fatalf("expected session to be loaded")
	}
}

func TestSessionStorePersistsExpiration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	store := newSessionStore(5*time.Millisecond, path)
	token, _ := store.create("alice")
	time.Sleep(10 * time.Millisecond)
	if _, ok := store.get(token); ok {
		t.Fatalf("expected session to expire")
	}
	loaded := newSessionStore(time.Hour, path)
	if _, ok := loaded.get(token); ok {
		t.Fatalf("expected expired session to be removed from persistence")
	}
}
