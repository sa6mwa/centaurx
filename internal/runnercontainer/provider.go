package runnercontainer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/runnergrpc"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

const (
	defaultLogBufferBytes = 128 * 1024
	defaultRunnerBinary   = "codex"
	defaultNamePrefix     = "centaurx-runner"
	defaultContainerHome  = "/centaurx"
)

type containerScope string

const (
	scopeUnknown containerScope = ""
	scopeUser    containerScope = "user"
	scopeTab     containerScope = "tab"
)

func parseContainerScope(value string) containerScope {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(scopeUser), "":
		return scopeUser
	case string(scopeTab):
		return scopeTab
	default:
		return scopeUnknown
	}
}

// Config configures the runner container provider.
type Config struct {
	Image             string
	RepoRoot          string
	RunnerRepoRoot    string
	HostRepoRoot      string
	HostStateDir      string
	SockDir           string
	StateDir          string
	SkelData          userhome.TemplateData
	SSHAgentDir       string
	RunnerBinary      string
	RunnerArgs        []string
	RunnerEnv         map[string]string
	GitSSHDebug       bool
	ContainerScope    string
	ExecNice          int
	CommandNice       int
	IdleTimeout       time.Duration
	KeepaliveInterval time.Duration
	KeepaliveMisses   int
	NamePrefix        string
	LogBufferBytes    int
	SocketWait        time.Duration
	SocketRetryWait   time.Duration
	CPUPercent        int
	MemoryPercent     int
}

// Provider manages per-tab runner containers.
type Provider struct {
	cfg               Config
	rt                shipohoy.Runtime
	agents            *sshagent.Manager
	logger            pslog.Logger
	skelDir           string
	hostRepoRoot      string
	hostSockDir       string
	hostAgentDir      string
	hostHomeRoot      string
	containerSockDir  string
	containerAgentDir string
	skelData          userhome.TemplateData
	scope             containerScope
	resourceCaps      *shipohoy.ResourceCaps

	mu   sync.Mutex
	tabs map[tabKey]*tabRunner
}

type logTailer interface {
	TailLogs(ctx context.Context, handle shipohoy.Handle, limit int) ([]string, []string, error)
}

type tabKey struct {
	user schema.UserID
	tab  schema.TabID
}

type tabRunner struct {
	client          *runnergrpc.Client
	handle          shipohoy.Handle
	info            core.RunnerInfo
	lastUsed        time.Time
	running         int
	keepaliveCancel context.CancelFunc

	wait chan struct{}
	err  error
	tabs map[schema.TabID]struct{}
}

// NewProvider constructs a Provider.
func NewProvider(ctx context.Context, cfg Config, rt shipohoy.Runtime, agents *sshagent.Manager) (*Provider, error) {
	if rt == nil {
		return nil, errors.New("runner runtime is required")
	}
	if agents == nil {
		return nil, errors.New("ssh agent manager is required")
	}
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, errors.New("runner image is required")
	}
	if strings.TrimSpace(cfg.RepoRoot) == "" {
		return nil, errors.New("repo root is required")
	}
	if strings.TrimSpace(cfg.RunnerRepoRoot) == "" {
		return nil, errors.New("runner repo root is required")
	}
	if strings.TrimSpace(cfg.SockDir) == "" {
		return nil, errors.New("runner socket directory is required")
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		return nil, errors.New("state directory is required")
	}
	if strings.TrimSpace(cfg.SSHAgentDir) == "" {
		return nil, errors.New("ssh agent directory is required")
	}
	scope := parseContainerScope(cfg.ContainerScope)
	if scope == scopeUnknown {
		return nil, fmt.Errorf("runner.container_scope must be \"user\" or \"tab\"")
	}
	caps := ResourceCapsFromPercent(cfg.CPUPercent, cfg.MemoryPercent, pslog.Ctx(ctx))

	if strings.TrimSpace(cfg.HostRepoRoot) != "" {
		runnerRoot := filepath.Clean(cfg.RunnerRepoRoot)
		hostRoot := filepath.Clean(cfg.HostRepoRoot)
		if runnerRoot == hostRoot {
			return nil, fmt.Errorf("runner repo root must be a container path; got runner_repo_root=%q host_repo_root=%q", cfg.RunnerRepoRoot, cfg.HostRepoRoot)
		}
	}
	if cfg.RunnerBinary == "" {
		cfg.RunnerBinary = defaultRunnerBinary
	}
	if cfg.NamePrefix == "" {
		cfg.NamePrefix = defaultNamePrefix
	}
	if cfg.LogBufferBytes <= 0 {
		cfg.LogBufferBytes = defaultLogBufferBytes
	}
	if cfg.SocketWait <= 0 {
		cfg.SocketWait = 30 * time.Second
	}
	if cfg.SocketRetryWait <= 0 {
		cfg.SocketRetryWait = 200 * time.Millisecond
	}

	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		return nil, err
	}
	if err := ensureDir(cfg.RepoRoot, 0o755); err != nil {
		return nil, fmt.Errorf("repo root %q: %w", cfg.RepoRoot, err)
	}
	if err := ensureDir(cfg.SockDir, 0o700); err != nil {
		return nil, fmt.Errorf("socket dir %q: %w", cfg.SockDir, err)
	}
	if err := ensureDir(cfg.SSHAgentDir, 0o700); err != nil {
		return nil, fmt.Errorf("ssh agent dir %q: %w", cfg.SSHAgentDir, err)
	}

	homeRoot := filepath.Join(cfg.StateDir, "home")
	if err := ensureDir(homeRoot, 0o700); err != nil {
		return nil, fmt.Errorf("runner home root %q: %w", homeRoot, err)
	}

	hostStateDir := cfg.HostStateDir
	if strings.TrimSpace(hostStateDir) == "" {
		hostStateDir = cfg.StateDir
	}
	hostRepoRoot := cfg.HostRepoRoot
	if strings.TrimSpace(hostRepoRoot) == "" {
		hostRepoRoot = cfg.RepoRoot
	}
	hostSockDir := resolveHostPath(hostStateDir, cfg.StateDir, cfg.SockDir)
	hostAgentDir := resolveHostPath(hostStateDir, cfg.StateDir, cfg.SSHAgentDir)
	hostHomeRoot := resolveHostPath(hostStateDir, cfg.StateDir, filepath.Join(cfg.StateDir, "home"))
	base := filepath.Dir(cfg.StateDir)
	skelDir := filepath.Join(base, "files", "skel")

	p := &Provider{
		cfg:               cfg,
		rt:                rt,
		agents:            agents,
		logger:            pslog.Ctx(ctx),
		skelDir:           skelDir,
		hostRepoRoot:      hostRepoRoot,
		hostSockDir:       hostSockDir,
		hostAgentDir:      hostAgentDir,
		hostHomeRoot:      hostHomeRoot,
		containerSockDir:  cfg.SockDir,
		containerAgentDir: cfg.SSHAgentDir,
		skelData:          cfg.SkelData,
		scope:             scope,
		resourceCaps:      caps,
		tabs:              make(map[tabKey]*tabRunner),
	}
	if cfg.IdleTimeout > 0 {
		go p.sweep(ctx, cfg.IdleTimeout)
	}
	return p, nil
}

// RunnerFor returns a runner for the specified tab.
func (p *Provider) RunnerFor(ctx context.Context, req core.RunnerRequest) (core.RunnerResponse, error) {
	if strings.TrimSpace(string(req.UserID)) == "" {
		if p != nil && p.logger != nil {
			p.logger.Warn("runner lookup rejected", "reason", "missing user")
		}
		return core.RunnerResponse{}, errors.New("user id is required")
	}
	if strings.TrimSpace(string(req.TabID)) == "" {
		if p != nil && p.logger != nil {
			p.logger.With("user", req.UserID).Warn("runner lookup rejected", "reason", "missing tab")
		}
		return core.RunnerResponse{}, errors.New("tab id is required")
	}
	key := p.keyFor(req.UserID, req.TabID)
	log := p.logger.With("user", req.UserID, "tab", req.TabID)

	p.mu.Lock()
	if entry, ok := p.tabs[key]; ok {
		wait := entry.wait
		p.mu.Unlock()
		if wait != nil {
			log.Debug("runner start in progress")
			select {
			case <-wait:
			case <-ctx.Done():
				return core.RunnerResponse{}, ctx.Err()
			}
		}
		p.mu.Lock()
		entry = p.tabs[key]
		if entry == nil {
			p.mu.Unlock()
			return core.RunnerResponse{}, errors.New("runner unavailable")
		}
		if entry.err != nil {
			err := entry.err
			delete(p.tabs, key)
			p.mu.Unlock()
			log.Warn("runner start failed", "err", err)
			return core.RunnerResponse{}, err
		}
		entry.lastUsed = time.Now()
		p.trackTabLocked(entry, req.TabID)
		resp := core.RunnerResponse{Runner: newTrackedRunner(entry.client, p, key, req.TabID), Info: entry.info}
		p.mu.Unlock()
		log.Debug("runner ready (cache hit)", "container", entry.handle.Name())
		return resp, nil
	}
	entry := &tabRunner{wait: make(chan struct{})}
	p.trackTabLocked(entry, req.TabID)
	p.tabs[key] = entry
	p.mu.Unlock()

	log.Info("runner start requested")
	client, info, handle, err := p.startRunner(ctx, key, req.TabID)
	p.mu.Lock()
	if err != nil {
		entry.err = err
		close(entry.wait)
		entry.wait = nil
		p.mu.Unlock()
		log.Warn("runner start failed", "err", err)
		return core.RunnerResponse{}, err
	}
	entry.client = client
	entry.handle = handle
	entry.info = info
	entry.keepaliveCancel = p.startKeepalive(key, client)
	entry.lastUsed = time.Now()
	close(entry.wait)
	entry.wait = nil
	p.mu.Unlock()
	log.Info("runner ready", "container", handle.Name(), "socket", filepath.Join(p.cfg.SockDir, string(key.user), string(key.tab), "runner.sock"))
	return core.RunnerResponse{Runner: newTrackedRunner(entry.client, p, key, req.TabID), Info: entry.info}, nil
}

func (p *Provider) keyFor(user schema.UserID, tab schema.TabID) tabKey {
	if p.scope == scopeTab {
		return tabKey{user: user, tab: tab}
	}
	return tabKey{user: user}
}

func (p *Provider) trackTabLocked(entry *tabRunner, tabID schema.TabID) {
	if p.scope != scopeUser || entry == nil || tabID == "" {
		return
	}
	if entry.tabs == nil {
		entry.tabs = make(map[schema.TabID]struct{})
	}
	entry.tabs[tabID] = struct{}{}
}

func (p *Provider) closeEntry(ctx context.Context, key tabKey, reason string) {
	p.mu.Lock()
	entry := p.tabs[key]
	if entry == nil {
		p.mu.Unlock()
		return
	}
	delete(p.tabs, key)
	p.mu.Unlock()
	p.logger.Info("runner stop requested", "user", key.user, "tab", key.tab, "reason", reason)
	_ = p.stopRunner(ctx, entry, key, reason)
}

// CloseTab stops and removes the runner for a tab.
func (p *Provider) CloseTab(ctx context.Context, req core.RunnerCloseRequest) error {
	if strings.TrimSpace(string(req.UserID)) == "" {
		if p != nil && p.logger != nil {
			p.logger.Warn("runner close rejected", "reason", "missing user")
		}
		return errors.New("user id is required")
	}
	if strings.TrimSpace(string(req.TabID)) == "" {
		if p != nil && p.logger != nil {
			p.logger.With("user", req.UserID).Warn("runner close rejected", "reason", "missing tab")
		}
		return errors.New("tab id is required")
	}
	key := p.keyFor(req.UserID, req.TabID)

	p.mu.Lock()
	entry := p.tabs[key]
	if entry == nil {
		p.mu.Unlock()
		return nil
	}
	if p.scope == scopeUser && req.TabID != "" {
		delete(entry.tabs, req.TabID)
		remaining := len(entry.tabs)
		if remaining > 0 {
			p.mu.Unlock()
			p.logger.Info("runner tab released", "user", req.UserID, "tab", req.TabID, "remaining", remaining)
			return nil
		}
	}
	delete(p.tabs, key)
	p.mu.Unlock()
	p.logger.Info("runner stop requested", "user", req.UserID, "tab", req.TabID)
	return p.stopRunner(ctx, entry, key, "closed")
}

// CloseAll stops and removes all runners.
func (p *Provider) CloseAll(ctx context.Context) error {
	p.mu.Lock()
	entries := make([]struct {
		key   tabKey
		entry *tabRunner
	}, 0, len(p.tabs))
	for key, entry := range p.tabs {
		entries = append(entries, struct {
			key   tabKey
			entry *tabRunner
		}{key: key, entry: entry})
	}
	p.tabs = make(map[tabKey]*tabRunner)
	p.mu.Unlock()
	p.logger.Info("runner close all requested", "count", len(entries))

	var lastErr error
	for _, item := range entries {
		if err := p.stopRunner(ctx, item.entry, item.key, "shutdown"); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (p *Provider) startRunner(ctx context.Context, key tabKey, logTab schema.TabID) (*runnergrpc.Client, core.RunnerInfo, shipohoy.Handle, error) {
	if logTab == "" {
		logTab = key.tab
	}
	log := p.logger.With("user", key.user, "tab", logTab)
	agentSock, err := p.agents.EnsureAgent(string(key.user))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Warn("ssh key missing; starting runner without agent")
		} else {
			return nil, core.RunnerInfo{}, nil, err
		}
	}

	repoRoot := filepath.Join(p.cfg.RepoRoot, string(key.user))
	if err := ensureDir(repoRoot, 0o755); err != nil {
		return nil, core.RunnerInfo{}, nil, fmt.Errorf("runner repo root %q: %w", repoRoot, err)
	}
	hostRepoRoot := filepath.Join(p.hostRepoRoot, string(key.user))
	if _, err := p.ensureHome(string(key.user)); err != nil {
		return nil, core.RunnerInfo{}, nil, err
	}
	hostHomePath := filepath.Join(p.hostHomeRoot, string(key.user))

	localSocketDir := filepath.Join(p.cfg.SockDir, string(key.user), string(key.tab))
	if err := ensureDir(localSocketDir, 0o700); err != nil {
		return nil, core.RunnerInfo{}, nil, fmt.Errorf("runner socket dir %q: %w", localSocketDir, err)
	}
	localSocketPath := filepath.Join(localSocketDir, "runner.sock")
	_ = os.Remove(localSocketPath)

	hostSocketDir := filepath.Join(p.hostSockDir, string(key.user), string(key.tab))

	containerSocketDir := path.Join(p.containerSockDir, string(key.user), string(key.tab))
	containerSocketPath := path.Join(containerSocketDir, "runner.sock")

	localAgentDir := filepath.Join(p.cfg.SSHAgentDir, string(key.user))
	if err := ensureDir(localAgentDir, 0o700); err != nil {
		return nil, core.RunnerInfo{}, nil, fmt.Errorf("runner agent dir %q: %w", localAgentDir, err)
	}
	hostAgentDir := filepath.Join(p.hostAgentDir, string(key.user))
	containerAgentDir := path.Join(p.containerAgentDir, string(key.user))
	containerAgentSock := ""
	if agentSock != "" {
		containerAgentSock = path.Join(containerAgentDir, "agent.sock")
	}

	containerRepoRoot := path.Join(p.cfg.RunnerRepoRoot, string(key.user))
	env := map[string]string{
		"HOME":            defaultContainerHome,
		"XDG_CACHE_HOME":  path.Join(defaultContainerHome, ".cache"),
		"XDG_CONFIG_HOME": path.Join(defaultContainerHome, ".config"),
		"XDG_DATA_HOME":   path.Join(defaultContainerHome, ".local", "share"),
		"CONAN_HOME":      path.Join(defaultContainerHome, ".conan2"),
	}
	if override, ok := p.cfg.RunnerEnv["GIT_SSH_COMMAND"]; ok && strings.TrimSpace(override) != "" {
		env["GIT_SSH_COMMAND"] = override
	} else {
		knownHostsPath := path.Join(defaultContainerHome, ".ssh", "known_hosts")
		env["GIT_SSH_COMMAND"] = defaultGitSSHCommand(containerAgentSock, knownHostsPath, p.cfg.GitSSHDebug)
	}
	if containerAgentSock != "" {
		env["SSH_AUTH_SOCK"] = containerAgentSock
	}

	spec := shipohoy.ContainerSpec{
		Name:           p.containerName(key),
		Image:          p.cfg.Image,
		Env:            env,
		Command:        p.runnerCommand(containerSocketPath),
		WorkingDir:     defaultContainerHome,
		ReadOnlyRootfs: true,
		AutoRemove:     true,
		LogBufferBytes: p.cfg.LogBufferBytes,
		ResourceCaps:   p.resourceCaps,
		Mounts: []shipohoy.Mount{
			{Source: hostRepoRoot, Target: containerRepoRoot, ReadOnly: false},
			{Source: hostHomePath, Target: defaultContainerHome, ReadOnly: false},
			{Source: hostSocketDir, Target: containerSocketDir, ReadOnly: false},
			{Source: hostAgentDir, Target: containerAgentDir, ReadOnly: false},
		},
		Tmpfs: []shipohoy.TmpfsMount{
			{Target: "/tmp", Options: []string{"mode=1777", "rw"}},
			{Target: "/run", Options: []string{"mode=0755", "rw"}},
			{Target: "/var/run", Options: []string{"mode=0755", "rw"}},
			{Target: "/var/tmp", Options: []string{"mode=1777", "rw"}},
		},
		Labels: map[string]string{
			"centaurx.user": string(key.user),
			"centaurx.tab":  string(key.tab),
		},
	}
	log = log.With("container", spec.Name)
	log.Trace("runner container spec", "image", spec.Image, "env_keys", len(spec.Env), "mounts", len(spec.Mounts), "tmpfs", len(spec.Tmpfs), "command_len", len(spec.Command))
	log.Trace("runner container command", "command", strings.Join(spec.Command, " "))
	log.Info("runner container ensure", "image", spec.Image, "repo_host", hostRepoRoot, "sock_host", hostSocketDir, "agent_host", hostAgentDir)
	handle, err := p.rt.EnsureRunning(ctx, spec)
	if err != nil {
		log.Warn("runner container ensure failed", "err", err)
		wrapped := fmt.Errorf("runner container start failed: %w (repo_host=%s sock_host=%s agent_host=%s)", err, hostRepoRoot, hostSocketDir, hostAgentDir)
		return nil, core.RunnerInfo{}, nil, core.NewRunnerError(core.RunnerErrorContainerStart, "container start", wrapped)
	}
	log.Info("runner container started", "id", handle.ID())

	waitCtx, cancel := context.WithTimeout(ctx, p.cfg.SocketWait)
	defer cancel()
	if err := waitForSocket(waitCtx, localSocketPath, p.cfg.SocketRetryWait); err != nil {
		log.Warn("runner socket wait failed", "err", err)
		p.logContainerTail(context.Background(), log, handle, "socket wait failed")
		_ = p.rt.Stop(context.Background(), handle)
		_ = p.rt.Remove(context.Background(), handle)
		return nil, core.RunnerInfo{}, nil, core.NewRunnerError(core.RunnerErrorContainerSocket, "socket wait", fmt.Errorf("runner socket not ready at %s: %w", localSocketPath, err))
	}
	log.Info("runner socket ready", "socket", localSocketPath)

	client, err := runnergrpc.Dial(ctx, localSocketPath)
	if err != nil {
		log.Warn("runner grpc dial failed", "err", err)
		p.logContainerTail(context.Background(), log, handle, "grpc dial failed")
		_ = p.rt.Stop(context.Background(), handle)
		_ = p.rt.Remove(context.Background(), handle)
		return nil, core.RunnerInfo{}, nil, err
	}
	info := core.RunnerInfo{
		RepoRoot:    p.cfg.RunnerRepoRoot,
		HomeDir:     defaultContainerHome,
		SSHAuthSock: containerAgentSock,
	}
	return client, info, handle, nil
}

func (p *Provider) stopRunner(ctx context.Context, entry *tabRunner, key tabKey, reason string) error {
	if entry == nil {
		return nil
	}
	log := p.logger.With("user", key.user, "tab", key.tab)
	if entry.handle != nil {
		log = log.With("container", entry.handle.Name())
	}
	if entry.client != nil {
		_ = entry.client.Close()
	}
	if entry.keepaliveCancel != nil {
		entry.keepaliveCancel()
	}
	if entry.handle != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := p.rt.Stop(stopCtx, entry.handle); err != nil {
			log.Warn("runner stop failed", "err", err)
		}
		if err := p.rt.Remove(stopCtx, entry.handle); err != nil {
			log.Warn("runner remove failed", "err", err)
		}
	}
	socketDir := filepath.Join(p.cfg.SockDir, string(key.user), string(key.tab))
	_ = os.RemoveAll(socketDir)
	log.Info("runner stopped", "reason", reason)
	return nil
}

func (p *Provider) logContainerTail(ctx context.Context, log pslog.Logger, handle shipohoy.Handle, reason string) {
	tailer, ok := p.rt.(logTailer)
	if !ok || tailer == nil || handle == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stdout, stderr, err := tailer.TailLogs(ctx, handle, 50)
	if err != nil {
		log.Warn("runner container logs unavailable", "reason", reason, "err", err)
		return
	}
	log.Warn("runner container logs", "reason", reason, "stdout_lines", len(stdout), "stderr_lines", len(stderr))
	if len(stdout) > 0 {
		payload, truncated := formatLogTail(stdout, 2000)
		log.Trace("runner container stdout tail", "truncated", truncated, "tail", payload)
	}
	if len(stderr) > 0 {
		payload, truncated := formatLogTail(stderr, 2000)
		log.Trace("runner container stderr tail", "truncated", truncated, "tail", payload)
	}
}

func formatLogTail(lines []string, maxBytes int) (string, bool) {
	payload := strings.Join(lines, "\n")
	if maxBytes <= 0 || len(payload) <= maxBytes {
		return payload, false
	}
	return payload[:maxBytes], true
}

func (p *Provider) startKeepalive(key tabKey, client *runnergrpc.Client) context.CancelFunc {
	if client == nil {
		return nil
	}
	interval := p.cfg.KeepaliveInterval
	misses := p.cfg.KeepaliveMisses
	if interval <= 0 || misses <= 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	log := p.logger.With("user", key.user, "tab", key.tab)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		consecutive := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, interval/2)
				err := client.Ping(pingCtx)
				pingCancel()
				if err != nil {
					consecutive++
					log.Warn("runner keepalive ping failed", "misses", consecutive, "err", err)
				} else {
					if consecutive > 0 {
						log.Debug("runner keepalive recovered", "misses", consecutive)
					}
					consecutive = 0
				}
				if consecutive >= misses {
					log.Warn("runner keepalive missed; closing runner", "misses", consecutive)
					go p.closeEntry(context.Background(), key, "keepalive missed")
					return
				}
			}
		}
	}()
	return cancel
}

func (p *Provider) containerName(key tabKey) string {
	user := sanitizeName(string(key.user))
	rawTab := strings.TrimSpace(string(key.tab))
	if rawTab == "" {
		return fmt.Sprintf("%s-%s", p.cfg.NamePrefix, user)
	}
	tab := sanitizeName(rawTab)
	return fmt.Sprintf("%s-%s-%s", p.cfg.NamePrefix, user, tab)
}

func (p *Provider) runnerCommand(socketPath string) []string {
	cmd := []string{"runner", "--socket-path", socketPath, "--binary", p.cfg.RunnerBinary}
	for _, arg := range p.cfg.RunnerArgs {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		cmd = append(cmd, "--arg", arg)
	}
	for _, env := range flattenEnv(p.cfg.RunnerEnv) {
		cmd = append(cmd, "--env", env)
	}
	if p.cfg.KeepaliveInterval > 0 {
		cmd = append(cmd, "--keepalive-interval", p.cfg.KeepaliveInterval.String())
	}
	if p.cfg.KeepaliveMisses > 0 {
		cmd = append(cmd, "--keepalive-misses", fmt.Sprintf("%d", p.cfg.KeepaliveMisses))
	}
	if p.cfg.ExecNice != 0 {
		cmd = append(cmd, "--exec-nice", fmt.Sprintf("%d", p.cfg.ExecNice))
	}
	if p.cfg.CommandNice != 0 {
		cmd = append(cmd, "--command-nice", fmt.Sprintf("%d", p.cfg.CommandNice))
	}
	return cmd
}

func (p *Provider) ensureHome(username string) (string, error) {
	return userhome.EnsureHome(p.cfg.StateDir, username, p.skelDir, p.skelData)
}

func (p *Provider) sweep(ctx context.Context, idle time.Duration) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.collectIdle(idle)
		}
	}
}

func (p *Provider) collectIdle(idle time.Duration) {
	now := time.Now()
	var toStop []struct {
		key   tabKey
		entry *tabRunner
	}
	p.mu.Lock()
	for key, entry := range p.tabs {
		if entry.wait != nil {
			p.logger.Debug("runner still starting", "user", key.user, "tab", key.tab)
			continue
		}
		if entry.running > 0 {
			p.logger.Debug("runner busy; skip idle", "user", key.user, "tab", key.tab, "running", entry.running)
			continue
		}
		if idle > 0 && now.Sub(entry.lastUsed) >= idle {
			delete(p.tabs, key)
			toStop = append(toStop, struct {
				key   tabKey
				entry *tabRunner
			}{key: key, entry: entry})
		}
	}
	p.mu.Unlock()
	for _, item := range toStop {
		p.logger.Info("runner idle timeout", "user", item.key.user, "tab", item.key.tab, "idle", idle)
		_ = p.stopRunner(context.Background(), item.entry, item.key, "idle")
	}
}

func waitForSocket(ctx context.Context, socketPath string, interval time.Duration) error {
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, key+"="+value)
	}
	return out
}

func sanitizeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func defaultGitSSHCommand(agentSock, knownHostsPath string, debug bool) string {
	args := []string{
		"ssh",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsPath),
		"-o", "IdentitiesOnly=no",
		"-o", "IdentityFile=/dev/null",
		"-o", "PreferredAuthentications=publickey",
	}
	if strings.TrimSpace(agentSock) != "" {
		args = append(args, "-o", fmt.Sprintf("IdentityAgent=%s", agentSock))
	}
	if debug {
		args = append(args, "-vvv", "-o", "LogLevel=DEBUG3")
	}
	return strings.Join(args, " ")
}

func resolveHostPath(hostStateDir, containerStateDir, containerPath string) string {
	if strings.TrimSpace(hostStateDir) == "" || strings.TrimSpace(containerStateDir) == "" {
		return containerPath
	}
	rel, err := filepath.Rel(containerStateDir, containerPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return containerPath
	}
	return filepath.Join(hostStateDir, rel)
}

func ensureDir(path string, mode fs.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is required")
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}

type trackedRunner struct {
	base     core.Runner
	provider *Provider
	key      tabKey
	logTab   schema.TabID
}

func newTrackedRunner(base core.Runner, provider *Provider, key tabKey, logTab schema.TabID) core.Runner {
	if logTab == "" {
		logTab = key.tab
	}
	return &trackedRunner{base: base, provider: provider, key: key, logTab: logTab}
}

func (t *trackedRunner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	t.provider.logger.Info("runner exec requested", "user", t.key.user, "tab", t.logTab, "model", req.Model, "resume", req.ResumeSessionID != "", "json", req.JSON, "prompt_len", len(req.Prompt))
	done := t.provider.startRun(t.key, t.logTab, "exec")
	handle, err := t.base.Run(ctx, req)
	if err != nil {
		done()
		return nil, err
	}
	return &trackedRunHandle{RunHandle: handle, done: done}, nil
}

func (t *trackedRunner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	t.provider.logger.Trace("runner command requested", "user", t.key.user, "tab", t.logTab, "shell", req.UseShell, "command_len", len(req.Command))
	done := t.provider.startRun(t.key, t.logTab, "command")
	handle, err := t.base.RunCommand(ctx, req)
	if err != nil {
		done()
		return nil, err
	}
	return &trackedCommandHandle{CommandHandle: handle, done: done}, nil
}

func (t *trackedRunner) Usage(ctx context.Context) (core.UsageInfo, error) {
	reader, ok := t.base.(core.UsageReader)
	if !ok {
		return core.UsageInfo{}, nil
	}
	t.provider.logger.Debug("runner usage requested", "user", t.key.user, "tab", t.logTab)
	return reader.Usage(ctx)
}

func (p *Provider) startRun(key tabKey, logTab schema.TabID, op string) func() {
	p.mu.Lock()
	entry := p.tabs[key]
	if entry != nil {
		entry.running++
		entry.lastUsed = time.Now()
	}
	p.mu.Unlock()
	if logTab == "" {
		logTab = key.tab
	}
	p.logger.Trace("runner activity start", "user", key.user, "tab", logTab, "op", op)
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			entry := p.tabs[key]
			if entry != nil {
				if entry.running > 0 {
					entry.running--
				}
				entry.lastUsed = time.Now()
			}
			p.mu.Unlock()
			p.logger.Trace("runner activity stop", "user", key.user, "tab", logTab, "op", op)
		})
	}
}

type trackedRunHandle struct {
	core.RunHandle
	done func()
	once sync.Once
}

func (h *trackedRunHandle) Wait(ctx context.Context) (core.RunResult, error) {
	result, err := h.RunHandle.Wait(ctx)
	h.once.Do(h.done)
	return result, err
}

func (h *trackedRunHandle) Done() <-chan struct{} {
	if done, ok := h.RunHandle.(interface{ Done() <-chan struct{} }); ok {
		return done.Done()
	}
	return nil
}

func (h *trackedRunHandle) Close() error {
	err := h.RunHandle.Close()
	h.once.Do(h.done)
	return err
}

type trackedCommandHandle struct {
	core.CommandHandle
	done func()
	once sync.Once
}

func (h *trackedCommandHandle) Wait(ctx context.Context) (core.RunResult, error) {
	result, err := h.CommandHandle.Wait(ctx)
	h.once.Do(h.done)
	return result, err
}

func (h *trackedCommandHandle) Done() <-chan struct{} {
	if done, ok := h.CommandHandle.(interface{ Done() <-chan struct{} }); ok {
		return done.Done()
	}
	return nil
}

func (h *trackedCommandHandle) Close() error {
	err := h.CommandHandle.Close()
	h.once.Do(h.done)
	return err
}
