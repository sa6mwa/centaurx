package sshkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestStoreGenerateLoadRotate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "keys.bundle"), filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	pub, err := store.GenerateKey("alice", KeyTypeEd25519, 0)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519") {
		t.Fatalf("expected ed25519 pub key, got %q", pub)
	}

	priv, err := store.LoadPrivateKey("alice")
	if err != nil {
		t.Fatalf("load private key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	derived := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if derived != strings.TrimSpace(pub) {
		t.Fatalf("public key mismatch")
	}

	pub2, err := store.RotateKey("alice", KeyTypeRSA, 2048)
	if err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if !strings.HasPrefix(pub2, "ssh-rsa") {
		t.Fatalf("expected rsa pub key, got %q", pub2)
	}
}

func TestStoreRemoveKey(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "keys.bundle"), filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.GenerateKey("bob", KeyTypeEd25519, 0); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := store.RemoveKey("bob"); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, err := store.LoadPrivateKey("bob"); err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist after removal, got %v", err)
	}
}
