package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
)

func TestGitSSHCloneWithHostKey(t *testing.T) {
	requireLong(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git not available")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Fatalf("ssh not available")
	}
	if _, err := exec.LookPath("git-upload-pack"); err != nil {
		t.Fatalf("git-upload-pack not available")
	}

	temp := t.TempDir()
	workDir := filepath.Join(temp, "work")
	repoDir := filepath.Join(temp, "repo.git")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("workdir: %v", err)
	}

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
		}
	}
	// Create source repo with a commit.
	run("init")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=centaurx", "-c", "user.email=dev@centaurx", "commit", "-m", "init")

	// Create bare repo and push.
	if out, err := exec.Command("git", "init", "--bare", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v (%s)", err, string(out))
	}
	run("remote", "add", "origin", repoDir)
	run("push", "origin", "HEAD:main")

	// Start mock SSH git server.
	hostSigner, hostPub, _ := newSigner(t)
	_, clientPub, clientPriv := newSigner(t)
	addr, stopServer := startGitSSHServer(t, temp, hostSigner, clientPub)
	defer stopServer()

	// Write client key to disk.
	keyPath := filepath.Join(temp, "id_ed25519")
	if err := writePrivateKey(keyPath, clientPriv); err != nil {
		t.Fatalf("write key: %v", err)
	}

	// Create known_hosts with server host key.
	hostPort := strings.TrimPrefix(addr, "tcp://")
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	knownHostLine := knownhosts.Line([]string{fmt.Sprintf("[%s]:%s", host, port)}, hostPub)
	knownHosts := filepath.Join(temp, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(knownHostLine+"\n"), 0o644); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cloneRoot := filepath.Join(temp, "repos")
	if err := os.MkdirAll(cloneRoot, 0o755); err != nil {
		t.Fatalf("repo root: %v", err)
	}

	sshCmd := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s -o LogLevel=ERROR", keyPath, knownHosts)
	oldSSHCmd := os.Getenv("GIT_SSH_COMMAND")
	_ = os.Setenv("GIT_SSH_COMMAND", sshCmd)
	defer func() {
		if oldSSHCmd == "" {
			_ = os.Unsetenv("GIT_SSH_COMMAND")
		} else {
			_ = os.Setenv("GIT_SSH_COMMAND", oldSSHCmd)
		}
	}()

	resolver, err := core.NewRunnerRepoResolver(cloneRoot, runnerProvider{runner: execRunner{}})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	cloneURL := fmt.Sprintf("ssh://git@%s/%s", hostPort, filepath.Base(repoDir))
	resp, err := resolver.OpenOrCloneURL(context.Background(), core.OpenOrCloneRequest{
		UserID: schema.UserID("tester"),
		TabID:  schema.TabID("tab1"),
		URL:    cloneURL,
	})
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}
	if !resp.Created {
		t.Fatalf("expected repo to be created")
	}
	if _, err := os.Stat(resp.Repo.Path); err != nil {
		t.Fatalf("expected repo path to exist: %v", err)
	}
}

type runnerProvider struct {
	runner core.Runner
}

func (p runnerProvider) RunnerFor(_ context.Context, _ core.RunnerRequest) (core.RunnerResponse, error) {
	return core.RunnerResponse{Runner: p.runner}, nil
}

func (p runnerProvider) CloseTab(context.Context, core.RunnerCloseRequest) error { return nil }
func (p runnerProvider) CloseAll(context.Context) error                          { return nil }

type execRunner struct{}

type execCommandHandle struct {
	lines    []core.CommandOutput
	exitCode int
}

type execCommandStream struct {
	lines []core.CommandOutput
	idx   int
}

func (s *execCommandStream) Next(context.Context) (core.CommandOutput, error) {
	if s.idx >= len(s.lines) {
		return core.CommandOutput{}, io.EOF
	}
	out := s.lines[s.idx]
	s.idx++
	return out, nil
}

func (s *execCommandStream) Close() error { return nil }

func (h *execCommandHandle) Outputs() core.CommandStream { return &execCommandStream{lines: h.lines} }
func (h *execCommandHandle) Signal(context.Context, core.ProcessSignal) error {
	return nil
}
func (h *execCommandHandle) Wait(context.Context) (core.RunResult, error) {
	return core.RunResult{ExitCode: h.exitCode}, nil
}
func (h *execCommandHandle) Close() error { return nil }

func (r execRunner) Run(context.Context, core.RunRequest) (core.RunHandle, error) {
	return nil, errors.New("unexpected Run")
}

func (r execRunner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	cmd, err := commandForRequest(req)
	if err != nil {
		return nil, err
	}
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	cmd.Env = os.Environ()
	if req.SSHAuthSock != "" {
		cmd.Env = append(filterEnv(cmd.Env, "SSH_AUTH_SOCK"), fmt.Sprintf("SSH_AUTH_SOCK=%s", req.SSHAuthSock))
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	lines := splitLines(out.String())
	return &execCommandHandle{lines: lines, exitCode: exitCode}, nil
}

func commandForRequest(req core.RunCommandRequest) (*exec.Cmd, error) {
	if req.UseShell {
		return exec.Command("sh", "-lc", req.Command), nil
	}
	parts := strings.Fields(req.Command)
	if len(parts) == 0 {
		return nil, errors.New("command is empty")
	}
	return exec.Command(parts[0], parts[1:]...), nil
}

func splitLines(raw string) []core.CommandOutput {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	out := make([]core.CommandOutput, 0, len(lines))
	for _, line := range lines {
		out = append(out, core.CommandOutput{Stream: core.CommandStreamStdout, Text: line})
	}
	return out
}

func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func newSigner(t *testing.T) (ssh.Signer, ssh.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer, signer.PublicKey(), priv
}

func writePrivateKey(path string, priv ed25519.PrivateKey) error {
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return err
	}
	data := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

func startGitSSHServer(t *testing.T, repoRoot string, hostSigner ssh.Signer, allowed ssh.PublicKey) (string, func()) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
			go handleGitConn(conn, cfg, repoRoot)
		}
	}()
	stopFn := func() {
		close(stop)
		_ = ln.Close()
		wg.Wait()
	}
	return "tcp://" + addr, stopFn
}

func handleGitConn(conn net.Conn, cfg *ssh.ServerConfig, repoRoot string) {
	defer conn.Close()
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go handleGitSession(channel, requests, repoRoot)
	}
}

func handleGitSession(channel ssh.Channel, requests <-chan *ssh.Request, repoRoot string) {
	defer channel.Close()
	for req := range requests {
		switch req.Type {
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			exitCode := runGitUploadPack(channel, repoRoot, payload.Command)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: uint32(exitCode)}))
			return
		case "env":
			_ = req.Reply(true, nil)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func runGitUploadPack(channel ssh.Channel, repoRoot, command string) int {
	cmd := strings.TrimSpace(command)
	prefix := "git-upload-pack"
	if !strings.HasPrefix(cmd, prefix) {
		_, _ = io.WriteString(channel, "unsupported command\n")
		return 1
	}
	arg := strings.TrimSpace(strings.TrimPrefix(cmd, prefix))
	arg = strings.Trim(arg, "'\"")
	if arg == "" {
		_, _ = io.WriteString(channel, "missing repo\n")
		return 1
	}
	repoPath := filepath.Clean(arg)
	if filepath.IsAbs(repoPath) {
		if _, err := os.Stat(repoPath); err != nil {
			repoPath = filepath.Join(repoRoot, strings.TrimPrefix(repoPath, string(filepath.Separator)))
		}
	} else {
		repoPath = filepath.Join(repoRoot, repoPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git-upload-pack", repoPath)
	c.Stdin = channel
	c.Stdout = channel
	c.Stderr = channel
	if err := c.Run(); err != nil {
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
