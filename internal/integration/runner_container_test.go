package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/runnercontainer"
	"pkt.systems/centaurx/internal/shipohoy/podman"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/schema"
)

func TestPodmanRunnerContainer(t *testing.T) {
	requireLong(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	repoRoot := filepath.Join(t.TempDir(), "repos")
	stateDir := filepath.Join(t.TempDir(), "state")
	sockDir := filepath.Join(stateDir, "runner")
	agentDir := filepath.Join(stateDir, "ssh", "agent")

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("state dir: %v", err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ssh key: %v", err)
	}
	agentMgr, err := sshagent.NewManager(staticKeyProvider{key: key}, agentDir)
	if err != nil {
		t.Fatalf("ssh agent: %v", err)
	}
	defer agentMgr.Close()

	runnerProvider, err := runnercontainer.NewProvider(ctx, runnercontainer.Config{
		Image:           image,
		RepoRoot:        repoRoot,
		RunnerRepoRoot:  "/repos",
		SockDir:         sockDir,
		StateDir:        stateDir,
		SSHAgentDir:     agentDir,
		RunnerBinary:    "codex",
		IdleTimeout:     0,
		SocketWait:      30 * time.Second,
		SocketRetryWait: 200 * time.Millisecond,
	}, rt, agentMgr)
	if err != nil {
		t.Fatalf("runner provider: %v", err)
	}

	user := schema.UserID("tester")
	tab := schema.TabID("runner-check")
	resp, err := runnerProvider.RunnerFor(ctx, core.RunnerRequest{UserID: user, TabID: tab})
	if err != nil {
		t.Fatalf("runner start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = runnerProvider.CloseTab(context.Background(), core.RunnerCloseRequest{UserID: user, TabID: tab})
	})

	workingDir := path.Join(resp.Info.RepoRoot, string(user))
	runCtx, cancelRun := context.WithTimeout(ctx, 20*time.Second)
	defer cancelRun()

	handle, err := resp.Runner.RunCommand(runCtx, core.RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     "pwd",
		UseShell:    true,
		SSHAuthSock: resp.Info.SSHAuthSock,
	})
	if err != nil {
		t.Fatalf("runner command start failed: %v", err)
	}
	defer handle.Close()

	stream := handle.Outputs()
	lines := []string{}
	for {
		line, err := stream.Next(runCtx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("command output read failed: %v", err)
		}
		if strings.TrimSpace(line.Text) != "" {
			lines = append(lines, line.Text)
		}
	}
	result, err := handle.Wait(runCtx)
	if err != nil {
		t.Fatalf("runner command failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("runner command exit %d (output: %s)", result.ExitCode, strings.Join(lines, "\n"))
	}
	if len(lines) == 0 {
		t.Fatalf("runner command produced no output")
	}
	if !strings.Contains(lines[len(lines)-1], "/repos") {
		t.Fatalf("unexpected working dir output: %s", strings.Join(lines, "\n"))
	}
}
