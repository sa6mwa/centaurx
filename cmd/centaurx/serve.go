package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx"
	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/httpapi"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/auth"
	"pkt.systems/centaurx/internal/runnercontainer"
	"pkt.systems/centaurx/internal/runnergrpc"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/schema"
	"pkt.systems/centaurx/sshserver"
	"pkt.systems/pslog"
)

//go:embed assets/LOGO-outrun02.txt
var serveLogo string

func newServeCmd() *cobra.Command {
	var cfgPath string
	var disableAuditTrails bool
	var noBanner bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start centaurx servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			logMode := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_MODE")))
			showBanner := !noBanner && logMode != "json" && logMode != "structured"
			if showBanner && serveLogo != "" {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), serveLogo)
			}
			logger := pslog.Ctx(cmd.Context())
			cfg, err := appconfig.Load(cfgPath)
			if err != nil {
				return err
			}
			if disableAuditTrails {
				cfg.Logging.DisableAuditTrails = true
			}
			if err := validateRunnerConfig(cfg); err != nil {
				return err
			}
			if err := ensureUserHomes(cfg, logger); err != nil {
				return err
			}
			switch cfg.Runner.Runtime {
			case "podman":
				logger.Info("runner runtime selected", "runtime", cfg.Runner.Runtime, "address", cfg.Runner.Podman.Address, "userns", cfg.Runner.Podman.UserNSMode)
			case "containerd":
				logger.Info("runner runtime selected", "runtime", cfg.Runner.Runtime, "address", cfg.Runner.Containerd.Address, "namespace", cfg.Runner.Containerd.Namespace)
			default:
				logger.Info("runner runtime selected", "runtime", cfg.Runner.Runtime)
			}
			rt, closeFn, err := selectRuntime(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if closeFn != nil {
				defer func() { _ = closeFn() }()
			}

			logger.Info("runner image verify start", "image", cfg.Runner.Image)
			if err := verifyRunnerImage(cmd.Context(), rt, cfg.Runner.Image); err != nil {
				return err
			}
			logger.Info("runner image verify ok", "image", cfg.Runner.Image)
			logger.Info("runner runtime verify start", "image", cfg.Runner.Image, "binary", cfg.Runner.Binary)
			if err := verifyRunnerRuntime(cmd.Context(), rt, cfg); err != nil {
				return err
			}
			logger.Info("runner runtime verify ok", "binary", cfg.Runner.Binary)

			serviceCfg := schema.ServiceConfig{
				RepoRoot:            cfg.RepoRoot,
				StateDir:            cfg.StateDir,
				DefaultModel:        schema.ModelID(cfg.Models.Default),
				AllowedModels:       toModelIDs(cfg.Models.Allowed),
				TabNameMax:          10,
				TabNameSuffix:       "$",
				BufferMaxLines:      cfg.Service.BufferMaxLines,
				DisableAuditLogging: cfg.Logging.DisableAuditTrails,
			}

			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			agentManager, err := sshagent.NewManagerWithLogger(keyStore, cfg.SSH.AgentDir, logger)
			if err != nil {
				return err
			}
			defer func() { _ = agentManager.Close() }()

			serverCfg := centaurx.ServerConfig{
				Service:             serviceCfg,
				HTTP:                toHTTPConfig(cfg.HTTP),
				SSH:                 toSSHConfig(cfg.SSH),
				Auth:                toAuthConfig(cfg.Auth),
				HubHistory:          1000,
				DisableAuditLogging: cfg.Logging.DisableAuditTrails,
			}
			runnerProvider, err := runnercontainer.NewProvider(cmd.Context(), runnercontainer.Config{
				Image:             cfg.Runner.Image,
				RepoRoot:          cfg.RepoRoot,
				RunnerRepoRoot:    cfg.Runner.RepoRoot,
				HostRepoRoot:      cfg.Runner.HostRepoRoot,
				HostStateDir:      cfg.Runner.HostStateDir,
				SockDir:           cfg.Runner.SockDir,
				StateDir:          cfg.StateDir,
				SkelData:          userhome.DefaultTemplateData(cfg),
				SSHAgentDir:       cfg.SSH.AgentDir,
				RunnerBinary:      cfg.Runner.Binary,
				RunnerArgs:        cfg.Runner.Args,
				RunnerEnv:         cfg.Runner.Env,
				GitSSHDebug:       cfg.Runner.GitSSHDebug,
				ContainerScope:    cfg.Runner.ContainerScope,
				ExecNice:          cfg.Runner.ExecNice,
				CommandNice:       cfg.Runner.CommandNice,
				IdleTimeout:       time.Duration(cfg.Runner.IdleTimeout) * time.Hour,
				KeepaliveInterval: time.Duration(cfg.Runner.KeepaliveIntervalSeconds) * time.Second,
				KeepaliveMisses:   cfg.Runner.KeepaliveMisses,
				CPUPercent:        cfg.Runner.Limits.CPUPercent,
				MemoryPercent:     cfg.Runner.Limits.MemoryPercent,
			}, rt, agentManager)
			if err != nil {
				return err
			}
			repoResolver, err := core.NewRunnerRepoResolver(cfg.RepoRoot, runnerProvider)
			if err != nil {
				return err
			}
			serverDeps := centaurx.ServerDeps{
				ServiceDeps: core.ServiceDeps{
					RunnerProvider: runnerProvider,
					RepoResolver:   repoResolver,
					Logger:         logger,
				},
			}
			server, err := centaurx.New(serverCfg, serverDeps, centaurx.WithHTTP(), centaurx.WithSSH())
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := server.Stop(stopCtx); err != nil {
					logger.Warn("server stop failed", "err", err)
				}
			}()
			logger.Info("http server listening", "addr", serverCfg.HTTP.Addr)
			logger.Info("ssh server listening", "addr", serverCfg.SSH.Addr)
			if err := server.Start(ctx); err != nil {
				return err
			}
			return server.Wait()
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "path to config file")
	cmd.Flags().BoolVar(&disableAuditTrails, "disable-audit-trails", false, "disable audit trail logging for commands")
	cmd.Flags().BoolVar(&noBanner, "no-banner", false, "disable startup banner")
	return cmd
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

func toHTTPConfig(cfg appconfig.HTTPConfig) httpapi.Config {
	return httpapi.Config{
		Addr:               cfg.Addr,
		SessionCookie:      cfg.SessionCookie,
		SessionTTLHours:    cfg.SessionTTLHours,
		SessionStorePath:   cfg.SessionStorePath,
		BaseURL:            cfg.BaseURL,
		BasePath:           cfg.BasePath,
		InitialBufferLines: cfg.InitialBufferLines,
		UIMaxBufferLines:   cfg.UIMaxBufferLines,
	}
}

func toSSHConfig(cfg appconfig.SSHConfig) sshserver.Config {
	return sshserver.Config{
		Addr:         cfg.Addr,
		HostKeyPath:  cfg.HostKeyPath,
		IdlePrompt:   "> ",
		KeyStorePath: cfg.KeyStorePath,
		KeyDir:       cfg.KeyDir,
	}
}

func toAuthConfig(cfg appconfig.AuthConfig) centaurx.AuthConfig {
	seeds := make([]centaurx.SeedUser, 0, len(cfg.SeedUsers))
	for _, seed := range cfg.SeedUsers {
		seeds = append(seeds, centaurx.SeedUser{
			Username:     seed.Username,
			PasswordHash: seed.PasswordHash,
			TOTPSecret:   seed.TOTPSecret,
		})
	}
	return centaurx.AuthConfig{
		UserFile:  cfg.UserFile,
		SeedUsers: seeds,
	}
}

func verifyRunnerImage(ctx context.Context, rt shipohoy.Runtime, image string) error {
	if strings.TrimSpace(image) == "" {
		return errors.New("runner.image is required")
	}
	if rt == nil {
		return errors.New("runner runtime is required")
	}
	checker, ok := rt.(interface {
		ImageExists(context.Context, string) (bool, error)
	})
	if !ok {
		return errors.New("runner runtime cannot verify images")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exists, err := checker.ImageExists(checkCtx, image)
	if err != nil {
		logger := pslog.Ctx(ctx).With("image", image)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			logger.Warn("runner image verify timed out; skipping check", "err", err)
			return nil
		}
		logger.Warn("runner image verify failed; skipping check", "err", err)
		return nil
	}
	if !exists {
		return fmt.Errorf("runner image %q not found; build it with: centaurx build runner --tag %s", image, image)
	}
	return nil
}

func ensureUserHomes(cfg appconfig.Config, logger pslog.Logger) error {
	store, err := auth.NewStoreWithLogger(cfg.Auth.UserFile, cfg.Auth.SeedUsers, logger)
	if err != nil {
		return err
	}
	users := store.LoadUsers()
	if len(users) == 0 {
		return nil
	}
	data := userhome.DefaultTemplateData(cfg)
	skelDir := userhome.SkelDir(cfg.StateDir)
	for _, user := range users {
		username := strings.TrimSpace(user.Username)
		if username == "" {
			continue
		}
		if _, err := userhome.EnsureHome(cfg.StateDir, username, skelDir, data); err != nil {
			return fmt.Errorf("user home %q: %w", username, err)
		}
	}
	return nil
}

func verifyRunnerRuntime(ctx context.Context, rt shipohoy.Runtime, cfg appconfig.Config) error {
	log := pslog.Ctx(ctx)
	verifyRoot := filepath.Join(cfg.StateDir, "verify", fmt.Sprintf("runner-%d", time.Now().UnixNano()))
	socketDir := filepath.Join(verifyRoot, "socket")
	homeDir := filepath.Join(verifyRoot, "home")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return fmt.Errorf("runner runtime verify socket dir: %w", err)
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return fmt.Errorf("runner runtime verify home dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(verifyRoot) }()

	containerHome := "/centaurx"
	containerSocketDir := path.Join(containerHome, "verify")
	socketPath := filepath.Join(socketDir, "runner.sock")
	containerSocketPath := path.Join(containerSocketDir, "runner.sock")

	env := map[string]string{
		"HOME":            containerHome,
		"XDG_CACHE_HOME":  path.Join(containerHome, ".cache"),
		"XDG_CONFIG_HOME": path.Join(containerHome, ".config"),
		"XDG_DATA_HOME":   path.Join(containerHome, ".local", "share"),
		"PATH":            "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	for key, value := range cfg.Runner.Env {
		if strings.TrimSpace(value) != "" {
			env[key] = value
		}
	}

	name := fmt.Sprintf("centaurx-verify-%d", time.Now().UnixNano())
	spec := shipohoy.ContainerSpec{
		Name:           name,
		Image:          cfg.Runner.Image,
		Command:        runnerVerifyCommand(cfg, containerSocketPath),
		Env:            env,
		WorkingDir:     containerHome,
		ReadOnlyRootfs: true,
		AutoRemove:     true,
		ResourceCaps:   runnercontainer.ResourceCapsFromPercent(cfg.Runner.Limits.CPUPercent, cfg.Runner.Limits.MemoryPercent, log),
		Mounts: []shipohoy.Mount{
			{Source: homeDir, Target: containerHome, ReadOnly: false},
			{Source: socketDir, Target: containerSocketDir, ReadOnly: false},
		},
		Tmpfs: []shipohoy.TmpfsMount{
			{Target: "/tmp", Options: []string{"mode=1777", "rw"}},
			{Target: "/run", Options: []string{"mode=0755", "rw"}},
			{Target: "/var/run", Options: []string{"mode=0755", "rw"}},
			{Target: "/var/tmp", Options: []string{"mode=1777", "rw"}},
		},
		Labels: map[string]string{"centaurx.check": "true"},
	}

	handle, err := rt.EnsureRunning(ctx, spec)
	if err != nil {
		return fmt.Errorf("runner runtime verify failed (start): %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = rt.Stop(stopCtx, handle)
		_ = rt.Remove(stopCtx, handle)
		stopCancel()
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()
	if err := waitForVerifySocket(waitCtx, socketPath, 200*time.Millisecond); err != nil {
		return fmt.Errorf("runner runtime verify failed (socket): %w", err)
	}

	client, err := runnergrpc.Dial(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("runner runtime verify failed (dial): %w", err)
	}
	defer func() { _ = client.Close() }()

	command := strings.TrimSpace(cfg.Runner.Binary)
	if command == "" {
		command = "codex"
	}
	runCtx, runCancel := context.WithTimeout(ctx, 15*time.Second)
	defer runCancel()
	handleCmd, err := client.RunCommand(runCtx, core.RunCommandRequest{
		WorkingDir: containerHome,
		Command:    fmt.Sprintf("%s -V", command),
		UseShell:   true,
	})
	if err != nil {
		return fmt.Errorf("runner runtime verify failed (command start): %w", err)
	}
	defer handleCmd.Close()

	var output bytes.Buffer
	stream := handleCmd.Outputs()
	for {
		line, err := stream.Next(runCtx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("runner runtime verify failed (command output): %w", err)
		}
		if line.Text != "" {
			output.WriteString(line.Text)
		}
	}
	result, err := handleCmd.Wait(runCtx)
	if err != nil {
		return fmt.Errorf("runner runtime verify failed (command wait): %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("runner runtime verify failed (%s -V exit %d): %s", command, result.ExitCode, strings.TrimSpace(output.String()))
	}
	return nil
}

func runnerVerifyCommand(cfg appconfig.Config, socketPath string) []string {
	command := []string{"runner", "--socket-path", socketPath, "--binary", cfg.Runner.Binary}
	for _, arg := range cfg.Runner.Args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		command = append(command, "--arg", arg)
	}
	for _, value := range flattenEnv(cfg.Runner.Env) {
		command = append(command, "--env", value)
	}
	if cfg.Runner.ExecNice != 0 {
		command = append(command, "--exec-nice", fmt.Sprintf("%d", cfg.Runner.ExecNice))
	}
	if cfg.Runner.CommandNice != 0 {
		command = append(command, "--command-nice", fmt.Sprintf("%d", cfg.Runner.CommandNice))
	}
	return command
}

func waitForVerifySocket(ctx context.Context, socketPath string, interval time.Duration) error {
	if strings.TrimSpace(socketPath) == "" {
		return errors.New("socket path is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(interval)
	}
}

func validateRunnerConfig(cfg appconfig.Config) error {
	if cfg.Runner.Runtime == "podman" {
		addr := strings.TrimSpace(cfg.Runner.Podman.Address)
		if addr == "" {
			return fmt.Errorf("runner.podman.address is required")
		}
		if strings.HasPrefix(addr, "unix:///cx/") {
			if strings.TrimSpace(cfg.Runner.HostStateDir) == "" || strings.TrimSpace(cfg.Runner.HostRepoRoot) == "" {
				return fmt.Errorf("runner.host_state_dir and runner.host_repo_root are required when using podman socket %q; set them to host paths (e.g. /home/%s/.centaurx/state)", addr, os.Getenv("USER"))
			}
		}
	}
	scope := strings.ToLower(strings.TrimSpace(cfg.Runner.ContainerScope))
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "tab" {
		return fmt.Errorf("runner.container_scope must be \"user\" or \"tab\"")
	}
	if cfg.Runner.CommandNice <= 0 {
		return fmt.Errorf("runner.command_nice must be greater than zero")
	}
	if cfg.Runner.ExecNice <= cfg.Runner.CommandNice {
		return fmt.Errorf("runner.exec_nice must be greater than runner.command_nice")
	}
	if cfg.Runner.Limits.CPUPercent < 0 || cfg.Runner.Limits.CPUPercent > 100 {
		return fmt.Errorf("runner.limits.cpu_percent must be between 0 and 100")
	}
	if cfg.Runner.Limits.MemoryPercent < 0 || cfg.Runner.Limits.MemoryPercent > 100 {
		return fmt.Errorf("runner.limits.memory_percent must be between 0 and 100")
	}
	hostRepoRoot := filepath.Clean(cfg.Runner.HostRepoRoot)
	runnerRepoRoot := filepath.Clean(cfg.Runner.RepoRoot)
	if hostRepoRoot != "" {
		prefix := hostRepoRoot + string(filepath.Separator)
		if runnerRepoRoot == hostRepoRoot || strings.HasPrefix(runnerRepoRoot, prefix) {
			return fmt.Errorf("runner.repo_root must be a container path (e.g. /repos); use runner.host_repo_root for host paths (got runner.repo_root=%q, runner.host_repo_root=%q)", cfg.Runner.RepoRoot, cfg.Runner.HostRepoRoot)
		}
	}
	return nil
}
