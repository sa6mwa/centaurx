package sshagent

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/crypto/ssh/agent"

	"pkt.systems/centaurx/internal/sshkeys"
)

func TestManagerEnsureAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ssh agent sockets are not supported on windows")
	}
	dir := t.TempDir()
	store, err := sshkeys.NewStore(filepath.Join(dir, "keys.bundle"), filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.GenerateKey("alice", sshkeys.KeyTypeEd25519, 0); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	manager, err := NewManager(store, filepath.Join(dir, "agent"))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	socket, err := manager.EnsureAgent("alice")
	if err != nil {
		t.Fatalf("ensure agent: %v", err)
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	client := agent.NewClient(conn)
	keys, err := client.List()
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	_ = conn.Close()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	if _, err := os.Stat(socket); err == nil {
		t.Fatalf("expected socket to be removed or inactive")
	}
}

func TestAgentSessionBindExtension(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ssh agent sockets are not supported on windows")
	}
	dir := t.TempDir()
	store, err := sshkeys.NewStore(filepath.Join(dir, "keys.bundle"), filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.GenerateKey("alice", sshkeys.KeyTypeEd25519, 0); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	manager, err := NewManager(store, filepath.Join(dir, "agent"))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	socket, err := manager.EnsureAgent("alice")
	if err != nil {
		t.Fatalf("ensure agent: %v", err)
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer conn.Close()

	client := agent.NewClient(conn)
	if _, err := client.Extension(sessionBindExtension, nil); err != nil {
		t.Fatalf("session bind extension failed: %v", err)
	}
}
