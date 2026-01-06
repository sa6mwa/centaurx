package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/schema"
)

func TestStoreRejectsInvalidUsername(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	store, err := NewStoreWithLogger(path, nil, nil)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if err := store.AddUser(User{
		Username:     "Alice",
		PasswordHash: "hash",
		TOTPSecret:   "secret",
	}); err == nil {
		t.Fatalf("expected invalid username error")
	}
}

func TestStoreRejectsInvalidSeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	_, err := NewStoreWithLogger(path, []appconfig.SeedUser{
		{
			Username:     "BadUser",
			PasswordHash: "hash",
			TOTPSecret:   "secret",
		},
	}, nil)
	if err == nil {
		t.Fatalf("expected error for invalid seed user")
	}
}

func TestStoreLoginPubKeysCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	store, err := NewStoreWithLogger(path, nil, nil)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.AddUser(User{
		Username:     "alice",
		PasswordHash: "hash",
		TOTPSecret:   "secret",
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	if _, err := store.AddLoginPubKey(schema.UserID("alice"), pubKey); err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}
	keys, err := store.ListLoginPubKeys(schema.UserID("alice"))
	if err != nil {
		t.Fatalf("list login pubkeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 pubkey, got %d", len(keys))
	}

	ok, err := store.HasLoginPubKey(schema.UserID("alice"), signer.PublicKey())
	if err != nil {
		t.Fatalf("has login pubkey: %v", err)
	}
	if !ok {
		t.Fatalf("expected stored pubkey to match")
	}

	if err := store.RemoveLoginPubKey(schema.UserID("alice"), 1); err != nil {
		t.Fatalf("remove login pubkey: %v", err)
	}
	keys, err = store.ListLoginPubKeys(schema.UserID("alice"))
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no pubkeys after remove, got %d", len(keys))
	}
}

func TestStoreChangePassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	store, err := NewStoreWithLogger(path, nil, nil)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	secret := "JBSWY3DPEHPK3PXP"
	hash, err := bcrypt.GenerateFromPassword([]byte("old-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := store.AddUser(User{
		Username:     "alice",
		PasswordHash: string(hash),
		TOTPSecret:   secret,
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp: %v", err)
	}
	if err := store.ChangePassword("alice", "old-pass", code, "new-pass"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if err := store.Authenticate("alice", "new-pass", code); err != nil {
		t.Fatalf("authenticate new password: %v", err)
	}
	if err := store.Authenticate("alice", "old-pass", code); err == nil {
		t.Fatalf("expected old password to fail")
	}
}
