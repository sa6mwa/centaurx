package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/schema"
	"pkt.systems/centaurx/sshserver"
)

func TestSSHSession(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	ts := newTestServer(t)
	signer := newTestSigner(t)
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), pubKey); err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}
	totpCode, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	client, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User: ts.user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
			ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
				return []string{totpCode}, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.RequestPty("xterm", 80, 40, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}

	output := &lockedBuffer{}
	go func() {
		_, _ = io.Copy(output, stdout)
	}()

	if _, err := fmt.Fprint(stdin, "/new demo\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "tab opened", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "hello\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "mock response: hello", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "/quit\r"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case <-time.After(5 * time.Second):
		t.Fatalf("session did not close after /quit")
	case <-done:
	}
}

func TestSSHQuitKeepsRunAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	signer := registerSSHLoginKey(t, ts)
	addr, cleanup := startSSHTestServer(t, ts)
	defer cleanup()

	client := dialSSH(t, addr, ts, signer)
	stdin, output, session := startSSHSession(t, client)

	if _, err := fmt.Fprint(stdin, "/new demo\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "tab opened", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "hello\r"); err != nil {
		t.Fatal(err)
	}
	waitForGateReady(t, runner.runGate, 5*time.Second)

	if _, err := fmt.Fprint(stdin, "/quit\r"); err != nil {
		t.Fatal(err)
	}
	waitForSessionClose(t, session)
	_ = client.Close()

	time.Sleep(200 * time.Millisecond)
	if err := runner.RunContextErr(); err != nil {
		t.Fatalf("expected run context to remain active after /quit, got %v", err)
	}
	runner.runGate.Release()

	client = dialSSH(t, addr, ts, signer)
	_, output, session = startSSHSession(t, client)
	expectOutput(t, output, "blocking response: hello", 5*time.Second)
	_ = session.Close()
	_ = client.Close()
}

func TestSSHQuitKeepsCommandAlive(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)
	runner := newBlockingRunner()
	ts := newTestServerWithRunner(t, runner)

	signer := registerSSHLoginKey(t, ts)
	addr, cleanup := startSSHTestServer(t, ts)
	defer cleanup()

	client := dialSSH(t, addr, ts, signer)
	stdin, output, session := startSSHSession(t, client)

	if _, err := fmt.Fprint(stdin, "/new demo\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "tab opened", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "! echo hello\r"); err != nil {
		t.Fatal(err)
	}
	waitForGateReady(t, runner.cmdGate, 5*time.Second)

	if _, err := fmt.Fprint(stdin, "/quit\r"); err != nil {
		t.Fatal(err)
	}
	waitForSessionClose(t, session)
	_ = client.Close()

	time.Sleep(200 * time.Millisecond)
	if err := runner.CommandContextErr(); err != nil {
		t.Fatalf("expected command context to remain active after /quit, got %v", err)
	}
	runner.cmdGate.Release()

	client = dialSSH(t, addr, ts, signer)
	_, output, session = startSSHSession(t, client)
	expectOutput(t, output, "blocking command: echo hello", 5*time.Second)
	_ = session.Close()
	_ = client.Close()
}

func expectOutput(t *testing.T, buffer *lockedBuffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content := buffer.String()
		if strings.Contains(content, substr) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	content := buffer.String()
	t.Fatalf("timeout waiting for %q in output: %s", substr, content)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func registerSSHLoginKey(t *testing.T, ts *testServer) ssh.Signer {
	t.Helper()
	signer := newTestSigner(t)
	pubKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if _, err := ts.authStore.AddLoginPubKey(schema.UserID(ts.user), pubKey); err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}
	return signer
}

func startSSHTestServer(t *testing.T, ts *testServer) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
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
	return ln.Addr().String(), func() {
		cancel()
		_ = ln.Close()
	}
}

func dialSSH(t *testing.T, addr string, ts *testServer, signer ssh.Signer) *ssh.Client {
	t.Helper()
	totpCode, err := totp.GenerateCode(ts.totp, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User: ts.user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
			ssh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) {
				return []string{totpCode}, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	return client
}

func startSSHSession(t *testing.T, client *ssh.Client) (io.WriteCloser, *lockedBuffer, *ssh.Session) {
	t.Helper()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RequestPty("xterm", 80, 40, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	output := &lockedBuffer{}
	go func() {
		_, _ = io.Copy(output, stdout)
	}()
	return stdin, output, session
}

func waitForSessionClose(t *testing.T, session *ssh.Session) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case <-time.After(5 * time.Second):
		t.Fatalf("session did not close after /quit")
	case <-done:
	}
}
