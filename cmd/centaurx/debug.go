package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/pslog"
)

func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug helpers for centaurx",
	}
	cmd.AddCommand(newDebugRunnerCmd())
	return cmd
}

func newDebugRunnerCmd() *cobra.Command {
	var cfgPath string
	var user string
	var tab string
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Print runner path mappings",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := pslog.Ctx(cmd.Context())
			cfg, err := appconfig.Load(cfgPath)
			if err != nil {
				return err
			}
			if err := validateRunnerConfig(cfg); err != nil {
				return err
			}
			hostStateDir := cfg.Runner.HostStateDir
			if strings.TrimSpace(hostStateDir) == "" {
				hostStateDir = cfg.StateDir
			}
			hostRepoRoot := cfg.Runner.HostRepoRoot
			if strings.TrimSpace(hostRepoRoot) == "" {
				hostRepoRoot = cfg.RepoRoot
			}
			hostSockDir := resolveHostPath(hostStateDir, cfg.StateDir, cfg.Runner.SockDir)

			logger.Info("runner debug config", "runtime", cfg.Runner.Runtime, "image", cfg.Runner.Image, "runner_repo_root", cfg.Runner.RepoRoot)
			logger.Info("runner debug paths", "state_dir", cfg.StateDir, "repo_root", cfg.RepoRoot, "sock_dir", cfg.Runner.SockDir)
			logger.Info("runner debug host paths", "host_state_dir", hostStateDir, "host_repo_root", hostRepoRoot, "host_sock_dir", hostSockDir)

			if strings.TrimSpace(user) != "" {
				hostRepoUser := filepath.Join(hostRepoRoot, user)
				hostSockUser := filepath.Join(hostSockDir, user)
				logger.Info("runner debug user paths", "user", user, "host_repo", hostRepoUser, "host_sock_dir", hostSockUser)
			}
			if strings.TrimSpace(tab) != "" && strings.TrimSpace(user) != "" {
				hostSockTab := filepath.Join(hostSockDir, user, tab)
				logger.Info("runner debug tab paths", "user", user, "tab", tab, "host_sock_tab", hostSockTab)
			}
			checkPath(logger, "host_repo_root", hostRepoRoot)
			checkPath(logger, "host_state_dir", hostStateDir)
			checkPath(logger, "host_sock_dir", hostSockDir)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "path to config file")
	cmd.Flags().StringVar(&user, "user", "", "user id to expand per-user paths")
	cmd.Flags().StringVar(&tab, "tab", "", "tab id to expand per-tab paths (requires --user)")
	return cmd
}

func resolveHostPath(hostState, containerState, value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	if strings.HasPrefix(value, containerState) {
		rel := strings.TrimPrefix(value, containerState)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		return filepath.Join(hostState, rel)
	}
	return value
}

func checkPath(logger pslog.Logger, label, value string) {
	if strings.TrimSpace(value) == "" {
		logger.Warn("path empty", "name", label)
		return
	}
	info, err := os.Stat(value)
	if err != nil {
		logger.Warn("path missing", "name", label, "path", value, "err", err)
		return
	}
	mode := info.Mode()
	logger.Info("path ok", "name", label, "path", value, "dir", mode.IsDir())
	if !mode.IsDir() {
		logger.Warn("path not directory", "name", label, "path", value)
	}
}
