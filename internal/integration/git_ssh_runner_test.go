package integration_test

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
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/runnercontainer"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/shipohoy/podman"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/schema"
)

func TestRunnerGitSSHDebugAgainstGoServer(t *testing.T) {
	requireLong(t)
	for _, bin := range []string{"git", "ssh", "git-upload-pack"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s not available", bin)
		}
	}

	cfg, err := appconfig.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	address := os.Getenv("CENTAURX_PODMAN_ADDRESS")
	if strings.TrimSpace(address) == "" {
		address = cfg.Runner.Podman.Address
	}
	image := os.Getenv("CENTAURX_RUNNER_IMAGE")
	if strings.TrimSpace(image) == "" {
		image = cfg.Runner.Image
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	rt, err := podman.New(ctx, podman.Config{Address: address, UserNSMode: cfg.Runner.Podman.UserNSMode})
	if err != nil {
		t.Fatalf("podman not available (%s): %v", address, err)
	}
	exists, err := rt.ImageExists(ctx, image)
	if err != nil {
		t.Fatalf("image check failed: %v", err)
	}
	if !exists {
		t.Fatalf("runner image %q not found; build the runner container first", image)
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

	hostSigner, hostPub, _ := newRunnerSigner(t)
	_, clientPub, clientPriv := newRunnerSigner(t)
	addr, stopServer := startRunnerGitSSHServer(t, temp, hostSigner, clientPub)
	defer stopServer()
	hostPort := strings.TrimPrefix(addr, "tcp://")

	repoRoot := filepath.Join(temp, "repos")
	stateDir := filepath.Join(temp, "state")
	sockDir := filepath.Join(stateDir, "runner")
	agentDir := filepath.Join(stateDir, "ssh", "agent")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("state dir: %v", err)
	}
	agentMgr, err := sshagent.NewManager(staticKeyProvider{key: clientPriv}, agentDir)
	if err != nil {
		t.Fatalf("ssh agent: %v", err)
	}
	defer agentMgr.Close()

	homeDir := filepath.Join(stateDir, "home", "tester")
	knownHosts := filepath.Join(homeDir, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(knownHosts), 0o700); err != nil {
		t.Fatalf("ssh dir: %v", err)
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	line := knownhosts.Line([]string{fmt.Sprintf("[%s]:%s", host, port)}, hostPub)
	if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	runnerProvider, err := runnercontainer.NewProvider(ctx, runnercontainer.Config{
		Image:           image,
		RepoRoot:        repoRoot,
		RunnerRepoRoot:  "/repos",
		SockDir:         sockDir,
		StateDir:        stateDir,
		SSHAgentDir:     agentDir,
		RunnerBinary:    "codex",
		GitSSHDebug:     true,
		ContainerScope:  "tab",
		IdleTimeout:     0,
		SocketWait:      30 * time.Second,
		SocketRetryWait: 200 * time.Millisecond,
		CPUPercent:      70,
		MemoryPercent:   70,
	}, hostNetworkRuntime{base: rt}, agentMgr)
	if err != nil {
		t.Fatalf("runner provider: %v", err)
	}

	user := schema.UserID("tester")
	tab := schema.TabID("git-ssh")
	resp, err := runnerProvider.RunnerFor(ctx, core.RunnerRequest{UserID: user, TabID: tab})
	if err != nil {
		t.Fatalf("runner start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = runnerProvider.CloseTab(context.Background(), core.RunnerCloseRequest{UserID: user, TabID: tab})
	})

	workingDir := path.Join(resp.Info.RepoRoot, string(user))
	cloneURL := fmt.Sprintf("ssh://git@%s/%s", hostPort, filepath.Base(repoDir))
	runCtx, cancelRun := context.WithTimeout(ctx, 45*time.Second)
	defer cancelRun()
	handle, err := resp.Runner.RunCommand(runCtx, core.RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     fmt.Sprintf("git clone %s repo", cloneURL),
		UseShell:    true,
		SSHAuthSock: resp.Info.SSHAuthSock,
	})
	if err != nil {
		t.Fatalf("runner command start failed: %v", err)
	}
	defer handle.Close()

	var output []string
	stream := handle.Outputs()
	for {
		line, err := stream.Next(runCtx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("command output read failed: %v", err)
		}
		if strings.TrimSpace(line.Text) != "" {
			output = append(output, line.Text)
		}
	}
	result, err := handle.Wait(runCtx)
	if err != nil {
		t.Fatalf("runner command failed: %v (%s)", err, strings.Join(output, "\n"))
	}
	if result.ExitCode != 0 {
		t.Fatalf("runner command exit %d (output: %s)", result.ExitCode, strings.Join(output, "\n"))
	}
	if !hasSSHDebugOutput(strings.Join(output, "\n")) {
		t.Fatalf("expected ssh debug output, got: %s", strings.Join(output, "\n"))
	}
	repoPath := filepath.Join(repoRoot, string(user), "repo", ".git")
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("expected repo at %s: %v", repoPath, err)
	}
}

type hostNetworkRuntime struct {
	base shipohoy.Runtime
}

func (r hostNetworkRuntime) EnsureImage(ctx context.Context, image string) error {
	return r.base.EnsureImage(ctx, image)
}
func (r hostNetworkRuntime) EnsureRunning(ctx context.Context, spec shipohoy.ContainerSpec) (shipohoy.Handle, error) {
	spec.HostNetwork = true
	return r.base.EnsureRunning(ctx, spec)
}
func (r hostNetworkRuntime) Stop(ctx context.Context, handle shipohoy.Handle) error {
	return r.base.Stop(ctx, handle)
}
func (r hostNetworkRuntime) Remove(ctx context.Context, handle shipohoy.Handle) error {
	return r.base.Remove(ctx, handle)
}
func (r hostNetworkRuntime) Exec(ctx context.Context, handle shipohoy.Handle, spec shipohoy.ExecSpec) (shipohoy.ExecResult, error) {
	return r.base.Exec(ctx, handle, spec)
}
func (r hostNetworkRuntime) WaitForPort(ctx context.Context, handle shipohoy.Handle, spec shipohoy.WaitPortSpec) error {
	return r.base.WaitForPort(ctx, handle, spec)
}
func (r hostNetworkRuntime) WaitForLog(ctx context.Context, handle shipohoy.Handle, spec shipohoy.WaitLogSpec) error {
	return r.base.WaitForLog(ctx, handle, spec)
}
func (r hostNetworkRuntime) Janitor(ctx context.Context, spec shipohoy.JanitorSpec) (int, error) {
	return r.base.Janitor(ctx, spec)
}

func hasSSHDebugOutput(output string) bool {
	return strings.Contains(output, "debug1:") ||
		strings.Contains(output, "debug2:") ||
		strings.Contains(output, "debug3:") ||
		strings.Contains(output, "Offering public key")
}

func newRunnerSigner(t *testing.T) (ssh.Signer, ssh.PublicKey, ed25519.PrivateKey) {
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

func startRunnerGitSSHServer(t *testing.T, repoRoot string, hostSigner ssh.Signer, allowed ssh.PublicKey) (string, func()) {
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
			go handleRunnerGitConn(conn, cfg, repoRoot)
		}
	}()
	stopFn := func() {
		close(stop)
		_ = ln.Close()
		wg.Wait()
	}
	return "tcp://" + addr, stopFn
}

func handleRunnerGitConn(conn net.Conn, cfg *ssh.ServerConfig, repoRoot string) {
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
		go handleRunnerGitSession(channel, requests, repoRoot)
	}
}

func handleRunnerGitSession(channel ssh.Channel, requests <-chan *ssh.Request, repoRoot string) {
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
			exitCode := runRunnerGitUploadPack(channel, repoRoot, payload.Command)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: uint32(exitCode)}))
			return
		case "env":
			_ = req.Reply(true, nil)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func runRunnerGitUploadPack(channel ssh.Channel, repoRoot, command string) int {
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
