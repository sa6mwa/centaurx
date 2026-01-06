package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/auth"
)

func TestUsersAddRejectsInvalidUsername(t *testing.T) {
	cfgPath := writeTestConfig(t)

	cmd := newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "add", "BadUser", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error for invalid username")
	}
}

func TestUsersAddAndDeleteValidUsername(t *testing.T) {
	cfgPath := writeTestConfig(t)
	cfg := loadConfigFromPath(t, cfgPath)

	cmd := newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "add", "alice.dev", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add user: %v", err)
	}

	store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	users := store.LoadUsers()
	if !hasUser(users, "alice.dev") {
		t.Fatalf("expected alice.dev in store, got %+v", users)
	}

	cmd = newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "delete", "alice.dev"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	store, err = auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if hasUser(store.LoadUsers(), "alice.dev") {
		t.Fatalf("expected alice.dev to be removed")
	}
	if _, err := os.Stat(filepath.Join(cfg.SSH.KeyDir, "alice.dev")); err == nil {
		t.Fatalf("expected ssh key material to be removed")
	}
}

func TestUsersRotateTOTP(t *testing.T) {
	cfgPath := writeTestConfig(t)
	cfg := loadConfigFromPath(t, cfgPath)

	cmd := newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "add", "bob", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add user: %v", err)
	}

	store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	orig := findUser(store.LoadUsers(), "bob")
	if orig == nil {
		t.Fatalf("expected bob user")
	}

	cmd = newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "rotate-totp", "bob"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rotate-totp: %v", err)
	}

	store, err = auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	updated := findUser(store.LoadUsers(), "bob")
	if updated == nil {
		t.Fatalf("expected bob user after rotate")
	}
	if updated.TOTPSecret == orig.TOTPSecret {
		t.Fatalf("expected TOTP secret to change")
	}
}

func TestUsersRotateSSHKey(t *testing.T) {
	cfgPath := writeTestConfig(t)

	cmd := newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "add", "dave", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add user: %v", err)
	}

	cmd = newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "rotate-ssh-key", "dave", "--ssh-key-type", "rsa", "--ssh-key-bits", "2048"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rotate-ssh-key: %v", err)
	}
}

func TestUsersChpasswd(t *testing.T) {
	cfgPath := writeTestConfig(t)
	cfg := loadConfigFromPath(t, cfgPath)

	cmd := newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "add", "carol", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add user: %v", err)
	}

	store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	orig := findUser(store.LoadUsers(), "carol")
	if orig == nil {
		t.Fatalf("expected carol user")
	}

	cmd = newUsersCmd()
	cmd.SetArgs([]string{"-c", cfgPath, "chpasswd", "carol", "--auto-password"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chpasswd: %v", err)
	}

	store, err = auth.NewStoreWithLogger(cfg.Auth.UserFile, nil, nil)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	updated := findUser(store.LoadUsers(), "carol")
	if updated == nil {
		t.Fatalf("expected carol user after chpasswd")
	}
	if updated.PasswordHash == orig.PasswordHash {
		t.Fatalf("expected password hash to change")
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	cfg, err := appconfig.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg.RepoRoot = t.TempDir()
	cfg.StateDir = t.TempDir()
	cfg.SSH.KeyStorePath = filepath.Join(t.TempDir(), "keys.bundle")
	cfg.SSH.KeyDir = filepath.Join(t.TempDir(), "keys")
	cfg.SSH.AgentDir = filepath.Join(t.TempDir(), "agent")
	cfg.Auth.UserFile = filepath.Join(t.TempDir(), "users.json")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func loadConfigFromPath(t *testing.T, path string) appconfig.Config {
	t.Helper()
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func hasUser(users []auth.User, username string) bool {
	for _, user := range users {
		if user.Username == username {
			return true
		}
	}
	return false
}

func findUser(users []auth.User, username string) *auth.User {
	for _, user := range users {
		if user.Username == username {
			copy := user
			return &copy
		}
	}
	return nil
}
