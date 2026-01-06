package runnercontainer

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/schema"
)

func TestResolveHostPath(t *testing.T) {
	host := "/host/state"
	container := "/cx/state"
	path := "/cx/state/runner"
	got := resolveHostPath(host, container, path)
	want := filepath.Join(host, "runner")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestEnsureDirRejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(file, 0o700); err == nil {
		t.Fatalf("expected error for file path")
	}
}

func TestNewProviderRejectsHostRepoRoot(t *testing.T) {
	temp := t.TempDir()
	repoRoot := filepath.Join(temp, "repos")
	stateDir := filepath.Join(temp, "state")
	agentDir := filepath.Join(temp, "agents")
	manager, err := sshagent.NewManager(fakeKeyProvider{}, agentDir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	cfg := Config{
		Image:          "test",
		RepoRoot:       repoRoot,
		RunnerRepoRoot: repoRoot,
		HostRepoRoot:   repoRoot,
		SockDir:        filepath.Join(stateDir, "sockets"),
		StateDir:       stateDir,
		SSHAgentDir:    filepath.Join(stateDir, "agents"),
	}
	if _, err := NewProvider(context.Background(), cfg, fakeRuntime{}, manager); err == nil {
		t.Fatalf("expected error for host repo root")
	}
}

func TestRunnerSpecSetsAutoRemove(t *testing.T) {
	temp := t.TempDir()
	repoRoot := filepath.Join(temp, "repos")
	stateDir := filepath.Join(temp, "state")
	agentDir := filepath.Join(stateDir, "agents")
	sockDir := filepath.Join(stateDir, "sockets")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("state dir: %v", err)
	}
	manager, err := sshagent.NewManager(fakeKeyProvider{}, agentDir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	user := schema.UserID("tester")
	tab := schema.TabID("tab1")
	hostSocketPath := filepath.Join(sockDir, string(user), string(tab), "runner.sock")
	if err := os.MkdirAll(filepath.Dir(hostSocketPath), 0o700); err != nil {
		t.Fatalf("socket dir: %v", err)
	}

	runtime := &captureRuntime{socketPath: hostSocketPath}
	provider, err := NewProvider(context.Background(), Config{
		Image:           "test",
		RepoRoot:        repoRoot,
		RunnerRepoRoot:  "/repos",
		HostRepoRoot:    repoRoot,
		SockDir:         sockDir,
		StateDir:        stateDir,
		SSHAgentDir:     agentDir,
		RunnerBinary:    "codex",
		SocketWait:      time.Second,
		SocketRetryWait: 10 * time.Millisecond,
	}, runtime, manager)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	if _, err := provider.RunnerFor(context.Background(), core.RunnerRequest{UserID: user, TabID: tab}); err != nil {
		t.Fatalf("runner for: %v", err)
	}
	if runtime.listener != nil {
		_ = runtime.listener.Close()
	}
	if runtime.lastSpec == nil {
		t.Fatalf("expected spec to be captured")
	}
	if !runtime.lastSpec.AutoRemove {
		t.Fatalf("expected AutoRemove=true")
	}
}

func TestRunnerUsesContainerAgentSock(t *testing.T) {
	temp := t.TempDir()
	hostStateDir := filepath.Join(temp, "hoststate")
	containerStateDir := filepath.Join(temp, "cxstate")
	if err := os.MkdirAll(hostStateDir, 0o700); err != nil {
		t.Fatalf("host state dir: %v", err)
	}
	if err := os.MkdirAll(containerStateDir, 0o700); err != nil {
		t.Fatalf("container state dir: %v", err)
	}

	hostAgentDir := filepath.Join(hostStateDir, "ssh", "agent")
	manager, err := sshagent.NewManager(fakeKeyProvider{}, hostAgentDir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	user := schema.UserID("tester")
	tab := schema.TabID("tab1")

	containerSockDir := filepath.Join(containerStateDir, "runner")
	hostSockDir := resolveHostPath(hostStateDir, containerStateDir, containerSockDir)
	hostSocketPath := filepath.Join(hostSockDir, string(user), string(tab), "runner.sock")
	if err := os.MkdirAll(filepath.Dir(hostSocketPath), 0o700); err != nil {
		t.Fatalf("socket dir: %v", err)
	}

	runtime := &captureRuntime{socketPath: hostSocketPath}
	containerAgentDir := filepath.Join(containerStateDir, "ssh", "agent")
	repoRoot := filepath.Join(temp, "repos")
	provider, err := NewProvider(context.Background(), Config{
		Image:           "test",
		RepoRoot:        repoRoot,
		RunnerRepoRoot:  "/repos",
		HostRepoRoot:    repoRoot,
		SockDir:         containerSockDir,
		StateDir:        containerStateDir,
		HostStateDir:    hostStateDir,
		SSHAgentDir:     containerAgentDir,
		RunnerBinary:    "codex",
		SocketWait:      time.Second,
		SocketRetryWait: 10 * time.Millisecond,
	}, runtime, manager)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	resp, err := provider.RunnerFor(context.Background(), core.RunnerRequest{UserID: user, TabID: tab})
	if err != nil {
		t.Fatalf("runner for: %v", err)
	}
	if runtime.lastSpec == nil {
		t.Fatalf("expected spec to be captured")
	}
	expectedSock := path.Join(containerAgentDir, string(user), "agent.sock")
	if resp.Info.SSHAuthSock != expectedSock {
		t.Fatalf("expected SSH auth sock %q, got %q", expectedSock, resp.Info.SSHAuthSock)
	}
	if got := runtime.lastSpec.Env["SSH_AUTH_SOCK"]; got != expectedSock {
		t.Fatalf("expected SSH_AUTH_SOCK %q, got %q", expectedSock, got)
	}
}

type fakeRuntime struct{}

func (fakeRuntime) EnsureImage(context.Context, string) error { return nil }
func (fakeRuntime) EnsureRunning(context.Context, shipohoy.ContainerSpec) (shipohoy.Handle, error) {
	return fakeHandle{}, nil
}
func (fakeRuntime) Stop(context.Context, shipohoy.Handle) error   { return nil }
func (fakeRuntime) Remove(context.Context, shipohoy.Handle) error { return nil }
func (fakeRuntime) Exec(context.Context, shipohoy.Handle, shipohoy.ExecSpec) (shipohoy.ExecResult, error) {
	return shipohoy.ExecResult{}, nil
}
func (fakeRuntime) WaitForPort(context.Context, shipohoy.Handle, shipohoy.WaitPortSpec) error {
	return nil
}
func (fakeRuntime) WaitForLog(context.Context, shipohoy.Handle, shipohoy.WaitLogSpec) error {
	return nil
}
func (fakeRuntime) Janitor(context.Context, shipohoy.JanitorSpec) (int, error) { return 0, nil }

type fakeHandle struct{}

func (fakeHandle) Name() string { return "fake" }
func (fakeHandle) ID() string   { return "fake" }

type captureRuntime struct {
	lastSpec   *shipohoy.ContainerSpec
	socketPath string
	listener   net.Listener
}

func (c *captureRuntime) EnsureImage(context.Context, string) error { return nil }
func (c *captureRuntime) EnsureRunning(_ context.Context, spec shipohoy.ContainerSpec) (shipohoy.Handle, error) {
	c.lastSpec = &spec
	if c.socketPath != "" && c.listener == nil {
		listener, err := net.Listen("unix", c.socketPath)
		if err != nil {
			return nil, err
		}
		c.listener = listener
	}
	return fakeHandle{}, nil
}
func (c *captureRuntime) Stop(context.Context, shipohoy.Handle) error   { return nil }
func (c *captureRuntime) Remove(context.Context, shipohoy.Handle) error { return nil }
func (c *captureRuntime) Exec(context.Context, shipohoy.Handle, shipohoy.ExecSpec) (shipohoy.ExecResult, error) {
	return shipohoy.ExecResult{}, nil
}
func (c *captureRuntime) WaitForPort(context.Context, shipohoy.Handle, shipohoy.WaitPortSpec) error {
	return nil
}
func (c *captureRuntime) WaitForLog(context.Context, shipohoy.Handle, shipohoy.WaitLogSpec) error {
	return nil
}
func (c *captureRuntime) Janitor(context.Context, shipohoy.JanitorSpec) (int, error) { return 0, nil }

type fakeKeyProvider struct{}

func (fakeKeyProvider) LoadPrivateKey(string) (crypto.PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return key, nil
}
