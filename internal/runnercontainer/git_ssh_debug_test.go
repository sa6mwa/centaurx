package runnercontainer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestGitSSHDebugOutputWithGoServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git ssh debug test in short mode")
	}
	for _, bin := range []string{"git", "ssh", "git-upload-pack"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}

	temp := t.TempDir()
	workDir := filepath.Join(temp, "work")
	repoDir := filepath.Join(temp, "repo.git")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("workdir: %v", err)
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
		}
	}
	runGit("init")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit("add", "README.md")
	runGit("-c", "user.name=centaurx", "-c", "user.email=dev@centaurx", "commit", "-m", "init")
	if out, err := exec.Command("git", "init", "--bare", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v (%s)", err, string(out))
	}
	runGit("remote", "add", "origin", repoDir)
	runGit("push", "origin", "HEAD:main")

	hostSigner, hostPub, _ := newSigner(t)
	_, clientPub, clientPriv := newSigner(t)
	addr, stopServer := startGitSSHServer(t, temp, hostSigner, clientPub)
	defer stopServer()

	hostPort := strings.TrimPrefix(addr, "tcp://")
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	knownHosts := filepath.Join(temp, "known_hosts")
	knownHostLine := knownhosts.Line([]string{fmt.Sprintf("[%s]:%s", host, port)}, hostPub)
	if err := os.WriteFile(knownHosts, []byte(knownHostLine+"\n"), 0o644); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	agentSock, stopAgent := startTestAgent(t, clientPriv)
	defer stopAgent()

	sshCmd := defaultGitSSHCommand(agentSock, knownHosts, true)
	cloneDir := filepath.Join(temp, "clone")
	cloneURL := fmt.Sprintf("ssh://git@%s/%s", hostPort, filepath.Base(repoDir))
	cmd := exec.Command("git", "clone", cloneURL, cloneDir)
	cmd.Env = append(os.Environ(),
		"GIT_SSH_COMMAND="+sshCmd,
		"SSH_AUTH_SOCK="+agentSock,
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clone failed: %v (%s)", err, string(out))
	}
	if !hasSSHDebugOutput(string(out)) {
		t.Fatalf("expected ssh debug output, got: %s", string(out))
	}
}

func hasSSHDebugOutput(output string) bool {
	return strings.Contains(output, "debug1:") ||
		strings.Contains(output, "debug2:") ||
		strings.Contains(output, "debug3:") ||
		strings.Contains(output, "Offering public key")
}

func startTestAgent(t *testing.T, priv ed25519.PrivateKey) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen agent: %v", err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		_ = listener.Close()
		t.Fatalf("agent add key: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_ = agent.ServeAgent(keyring, c)
				_ = c.Close()
			}(conn)
		}
	}()
	stop := func() {
		_ = listener.Close()
		<-done
		_ = os.Remove(socket)
	}
	return socket, stop
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
