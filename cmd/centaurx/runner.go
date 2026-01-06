package main

import (
	"errors"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/internal/codex"
	"pkt.systems/centaurx/internal/runnerconfig"
	"pkt.systems/centaurx/internal/runnergrpc"
	"pkt.systems/pslog"
)

func newRunnerCmd() *cobra.Command {
	var cfgPath string
	var socketPath string
	var binaryPath string
	var runnerArgs []string
	var runnerEnv []string
	var keepaliveInterval time.Duration
	var keepaliveMisses int
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Start the runner daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := pslog.Ctx(cmd.Context())

			cfg, err := loadRunnerConfig(cfgPath, socketPath, binaryPath, runnerArgs, runnerEnv, keepaliveInterval, keepaliveMisses)
			if err != nil {
				return err
			}
			logger.Info("runner config loaded", "binary", cfg.Binary, "args", len(cfg.Args), "env", len(cfg.Env), "keepalive_interval", cfg.KeepaliveInterval, "keepalive_misses", cfg.KeepaliveMisses)

			runner, err := codex.NewRunner(codex.Config{
				BinaryPath: cfg.Binary,
				ExtraArgs:  cfg.Args,
				Env:        cfg.Env,
			})
			if err != nil {
				return err
			}

			server := runnergrpc.NewServer(runnergrpc.Config{
				SocketPath:        cfg.SocketPath,
				KeepaliveInterval: cfg.KeepaliveInterval,
				KeepaliveMisses:   cfg.KeepaliveMisses,
			}, runner)

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			logger.Info("runner binary ready", "path", cfg.Binary)
			logger.Info("runner socket listening", "socket", cfg.SocketPath)
			return server.ListenAndServe(ctx)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "path to config file")
	cmd.Flags().StringVar(&socketPath, "socket-path", "", "runner socket path (overrides config)")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "codex binary path (overrides config)")
	cmd.Flags().StringArrayVar(&runnerArgs, "arg", nil, "extra codex args (repeatable)")
	cmd.Flags().StringArrayVar(&runnerEnv, "env", nil, "extra env for codex (repeatable KEY=VAL)")
	cmd.Flags().DurationVar(&keepaliveInterval, "keepalive-interval", 0, "runner keepalive interval (e.g. 10s)")
	cmd.Flags().IntVar(&keepaliveMisses, "keepalive-misses", 0, "runner keepalive misses before exit")
	return cmd
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

type runnerConfig struct {
	SocketPath        string
	Binary            string
	Args              []string
	Env               []string
	KeepaliveInterval time.Duration
	KeepaliveMisses   int
}

func loadRunnerConfig(cfgPath, socketPath, binaryPath string, args, env []string, keepaliveInterval time.Duration, keepaliveMisses int) (runnerConfig, error) {
	if strings.TrimSpace(cfgPath) == "" {
		cfg := runnerConfig{
			SocketPath:        socketPath,
			Binary:            binaryPath,
			Args:              args,
			Env:               env,
			KeepaliveInterval: keepaliveInterval,
			KeepaliveMisses:   keepaliveMisses,
		}
		if strings.TrimSpace(cfg.SocketPath) == "" {
			return runnerConfig{}, errors.New("socket path is required")
		}
		if strings.TrimSpace(cfg.Binary) == "" {
			cfg.Binary = "codex"
		}
		if cfg.KeepaliveInterval == 0 {
			cfg.KeepaliveInterval = 10 * time.Second
		}
		if cfg.KeepaliveMisses == 0 {
			cfg.KeepaliveMisses = 3
		}
		return cfg, nil
	}

	fileCfg, err := runnerconfig.Load(cfgPath)
	if err != nil {
		return runnerConfig{}, err
	}
	if strings.TrimSpace(socketPath) != "" {
		fileCfg.SocketPath = socketPath
	}
	if strings.TrimSpace(binaryPath) != "" {
		fileCfg.Binary = binaryPath
	}
	if len(args) > 0 {
		fileCfg.Args = args
	}
	if len(env) > 0 {
		fileCfg.Env = mapFromEnv(env)
	}
	if keepaliveInterval > 0 {
		fileCfg.KeepaliveIntervalSeconds = int(keepaliveInterval.Seconds())
	}
	if keepaliveMisses > 0 {
		fileCfg.KeepaliveMisses = keepaliveMisses
	}

	return runnerConfig{
		SocketPath:        fileCfg.SocketPath,
		Binary:            fileCfg.Binary,
		Args:              fileCfg.Args,
		Env:               flattenEnv(fileCfg.Env),
		KeepaliveInterval: time.Duration(fileCfg.KeepaliveIntervalSeconds) * time.Second,
		KeepaliveMisses:   fileCfg.KeepaliveMisses,
	}, nil
}

func mapFromEnv(values []string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = val
	}
	return out
}
