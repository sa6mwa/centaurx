package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/internal/auth"
	"pkt.systems/centaurx/schema"
	"pkt.systems/centaurx/sshserver"
)

func TestSSHAuthRequiresPubKeyAndTotp(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)
	signer := newTestSigner(t)
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), pubKey); err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}

	addr, stop := startSSHServer(t, ts)
	defer stop()

	if _, err := sshDial(addr, ts.user, []ssh.AuthMethod{ssh.PublicKeys(signer)}); err == nil {
		t.Fatalf("expected auth failure without TOTP")
	}

	if _, err := sshDial(addr, ts.user, []ssh.AuthMethod{
		ssh.PublicKeys(signer),
		ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
			return []string{"000000"}, nil
		}),
	}); err == nil {
		t.Fatalf("expected auth failure with wrong TOTP")
	}

	code, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	var prompts []string
	var echoes []bool
	client, err := sshDial(addr, ts.user, []ssh.AuthMethod{
		ssh.PublicKeys(signer),
		ssh.KeyboardInteractive(func(_, _ string, questions []string, echos []bool) ([]string, error) {
			prompts = append(prompts, questions...)
			echoes = append(echoes, echos...)
			return []string{code}, nil
		}),
	})
	if err != nil {
		t.Fatalf("expected auth success with pubkey+totp: %v", err)
	}
	_ = client.Close()

	if len(prompts) != 1 || prompts[0] != "Verification code: " {
		t.Fatalf("unexpected prompt: %#v", prompts)
	}
	if len(echoes) != 1 || echoes[0] {
		t.Fatalf("expected no-echo prompt, got %#v", echoes)
	}
}

func TestSSHAuthRejectsWrongPubKey(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)
	allowed := newTestSigner(t)
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(allowed.PublicKey())))
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), pubKey); err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}

	addr, stop := startSSHServer(t, ts)
	defer stop()

	code, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}

	var prompted bool
	other := newTestSigner(t)
	if _, err := sshDial(addr, ts.user, []ssh.AuthMethod{
		ssh.PublicKeys(other),
		ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
			prompted = true
			return []string{code}, nil
		}),
	}); err == nil {
		t.Fatalf("expected auth failure with wrong pubkey")
	}
	if prompted {
		t.Fatalf("unexpected TOTP prompt without valid pubkey")
	}
}

func TestSSHAuthAllowsAnyUserPubKey(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)
	keyA := newTestSigner(t)
	keyB := newTestSigner(t)
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(keyA.PublicKey())))); err != nil {
		t.Fatalf("add pubkey A: %v", err)
	}
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(keyB.PublicKey())))); err != nil {
		t.Fatalf("add pubkey B: %v", err)
	}

	addr, stop := startSSHServer(t, ts)
	defer stop()

	code, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}

	client, err := sshDial(addr, ts.user, []ssh.AuthMethod{
		ssh.PublicKeys(keyB),
		ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
			return []string{code}, nil
		}),
	})
	if err != nil {
		t.Fatalf("expected auth success with secondary pubkey: %v", err)
	}
	_ = client.Close()
}

func TestSSHAuthRejectsUnknownUser(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)
	addr, stop := startSSHServer(t, ts)
	defer stop()

	key := newTestSigner(t)
	var prompted bool
	if _, err := sshDial(addr, "unknown", []ssh.AuthMethod{
		ssh.PublicKeys(key),
		ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
			prompted = true
			return []string{"000000"}, nil
		}),
	}); err == nil {
		t.Fatalf("expected auth failure for unknown user")
	}
	if prompted {
		t.Fatalf("unexpected TOTP prompt for unknown user")
	}
}

func TestSSHAuthRejectsCrossUserKey(t *testing.T) {
	requireLong(t)
	ts := newTestServer(t)
	otherUser := "other"
	if err := ts.authStore.AddUser(auth.User{
		Username:     otherUser,
		PasswordHash: "hash",
		TOTPSecret:   "secret",
	}); err != nil {
		t.Fatalf("add other user: %v", err)
	}

	otherKey := newTestSigner(t)
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(otherUser), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(otherKey.PublicKey())))); err != nil {
		t.Fatalf("add other pubkey: %v", err)
	}

	addr, stop := startSSHServer(t, ts)
	defer stop()

	code, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}

	if _, err := sshDial(addr, ts.user, []ssh.AuthMethod{
		ssh.PublicKeys(otherKey),
		ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
			return []string{code}, nil
		}),
	}); err == nil {
		t.Fatalf("expected auth failure with cross-user key")
	}
}

func startSSHServer(t *testing.T, ts *testServer) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	hostKey := fmt.Sprintf("%s/host_key", t.TempDir())
	server := &sshserver.Server{
		Addr:        ln.Addr().String(),
		Listener:    ln,
		HostKeyPath: hostKey,
		Service:     ts.service,
		Handler:     ts.handler,
		AuthStore:   ts.authStore,
	}
	go func() {
		_ = server.ListenAndServe(ctx)
	}()

	stop := func() {
		cancel()
		_ = ln.Close()
	}
	return ln.Addr().String(), stop
}

func sshDial(addr, user string, methods []ssh.AuthMethod) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}

func newTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}
