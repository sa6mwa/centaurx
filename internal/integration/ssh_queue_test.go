package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
	"pkt.systems/centaurx/sshserver"
)

func TestSSHQueuedPrompt(t *testing.T) {
	requireLong(t)
	ensureGitAvailable(t)

	runner := newSlowRunner()
	ts := newTestServerWithRunner(t, runner)
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

	if _, err := fmt.Fprint(stdin, "first\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "> first", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "second\r"); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, output, "queued prompt: second", 5*time.Second)

	runner.Release()
	expectOutput(t, output, "mock response: first", 5*time.Second)

	runner.Release()
	expectOutput(t, output, "mock response: second", 5*time.Second)

	if _, err := fmt.Fprint(stdin, "/quit\r"); err != nil {
		t.Fatal(err)
	}
}

type slowRunner struct {
	counter uint64
	release chan struct{}
}

func newSlowRunner() *slowRunner {
	return &slowRunner{release: make(chan struct{}, 4)}
}

func (r *slowRunner) Release() {
	r.release <- struct{}{}
}

func (r *slowRunner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	id := atomic.AddUint64(&r.counter, 1)
	threadID := req.ResumeSessionID
	if threadID == "" {
		threadID = schema.SessionID(fmt.Sprintf("slow-thread-%d", id))
	}
	stream := &slowStream{
		threadID: threadID,
		prompt:   req.Prompt,
		release:  r.release,
		done:     make(chan struct{}),
	}
	return &slowHandle{stream: stream}, nil
}

func (r *slowRunner) RunCommand(context.Context, core.RunCommandRequest) (core.CommandHandle, error) {
	return nil, fmt.Errorf("commands not supported")
}

type slowHandle struct {
	stream *slowStream
}

func (h *slowHandle) Events() core.EventStream {
	return h.stream
}

func (h *slowHandle) Signal(context.Context, core.ProcessSignal) error {
	return nil
}

func (h *slowHandle) Wait(ctx context.Context) (core.RunResult, error) {
	select {
	case <-h.stream.done:
		return core.RunResult{ExitCode: 0}, nil
	case <-ctx.Done():
		return core.RunResult{}, ctx.Err()
	}
}

func (h *slowHandle) Close() error {
	h.stream.closeDone()
	return nil
}

type slowStream struct {
	threadID schema.SessionID
	prompt   string
	index    int
	release  chan struct{}
	done     chan struct{}
}

func (s *slowStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	if ctx.Err() != nil {
		return schema.ExecEvent{}, ctx.Err()
	}
	switch s.index {
	case 0:
		s.index++
		return schema.ExecEvent{Type: schema.EventThreadStarted, ThreadID: s.threadID}, nil
	case 1:
		select {
		case <-s.release:
		case <-ctx.Done():
			return schema.ExecEvent{}, ctx.Err()
		}
		s.index++
		return schema.ExecEvent{
			Type: schema.EventItemCompleted,
			Item: &schema.ItemEvent{Type: schema.ItemAgentMessage, Text: fmt.Sprintf("mock response: %s", s.prompt)},
		}, nil
	case 2:
		s.index++
		return schema.ExecEvent{Type: schema.EventTurnCompleted}, nil
	default:
		s.closeDone()
		return schema.ExecEvent{}, io.EOF
	}
}

func (s *slowStream) Close() error {
	s.closeDone()
	return nil
}

func (s *slowStream) closeDone() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}
