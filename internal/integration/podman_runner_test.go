package integration_test

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/command"
	"pkt.systems/centaurx/internal/runnercontainer"
	"pkt.systems/centaurx/internal/shipohoy/podman"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/schema"
)

func TestPodmanRunnerNew(t *testing.T) {
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
		ContainerScope:  "tab",
		IdleTimeout:     0,
		SocketWait:      10 * time.Second,
		SocketRetryWait: 200 * time.Millisecond,
		CPUPercent:      70,
		MemoryPercent:   70,
	}, rt, agentMgr)
	if err != nil {
		t.Fatalf("runner provider: %v", err)
	}

	repoResolver, err := core.NewRunnerRepoResolver(repoRoot, runnerProvider)
	if err != nil {
		t.Fatalf("repo resolver: %v", err)
	}
	service, err := core.NewService(schema.ServiceConfig{
		RepoRoot:      repoRoot,
		StateDir:      stateDir,
		DefaultModel:  schema.ModelID(cfg.Models.Default),
		AllowedModels: toModelIDs(cfg.Models.Allowed),
		TabNameMax:    10,
		TabNameSuffix: "$",
	}, core.ServiceDeps{
		RunnerProvider: runnerProvider,
		RepoResolver:   repoResolver,
	})
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	user := schema.UserID("tester")
	t.Cleanup(func() {
		listResp, err := service.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
		if err != nil {
			t.Logf("cleanup list tabs failed: %v", err)
			return
		}
		for _, tab := range listResp.Tabs {
			_, err := service.CloseTab(context.Background(), schema.CloseTabRequest{UserID: user, TabID: tab.ID})
			if err != nil {
				t.Logf("cleanup close tab %s failed: %v", tab.ID, err)
			}
		}
	})

	handler := command.NewHandler(service, runnerProvider, command.HandlerConfig{
		AllowedModels: toModelIDs(cfg.Models.Allowed),
		RepoRoot:      repoRoot,
	})
	handled, err := handler.Handle(ctx, user, "", "/new demo")
	if err != nil {
		t.Fatalf("command /new failed: %v (ensure podman is running and the runner image can start)", err)
	}
	if !handled {
		t.Fatalf("expected /new to be handled")
	}

	repoPath := filepath.Join(repoRoot, string(user), "demo", ".git")
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("expected repo at %s: %v", repoPath, err)
	}
}

type staticKeyProvider struct {
	key ed25519.PrivateKey
}

func (p staticKeyProvider) LoadPrivateKey(_ string) (crypto.PrivateKey, error) {
	return p.key, nil
}

func toModelIDs(values []string) []schema.ModelID {
	if len(values) == 0 {
		return nil
	}
	out := make([]schema.ModelID, 0, len(values))
	for _, value := range values {
		out = append(out, schema.ModelID(value))
	}
	return out
}
