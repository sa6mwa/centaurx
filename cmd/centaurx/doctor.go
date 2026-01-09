package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/runnercontainer"
	"pkt.systems/centaurx/internal/sshagent"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

func newDoctorCmd() *cobra.Command {
	var cfgPath string
	var user string
	var codexPrompt string
	var commandTimeout time.Duration
	var codexTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run centaurx diagnostics",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := pslog.Ctx(cmd.Context())

			cfg, err := appconfig.Load(cfgPath)
			if err != nil {
				return err
			}
			configPath := cfgPath
			if strings.TrimSpace(configPath) == "" {
				path, err := appconfig.DefaultConfigPath()
				if err != nil {
					return err
				}
				configPath = path
			}
			logger.Info("doctor start", "config", configPath)

			if err := validateRunnerConfig(cfg); err != nil {
				return err
			}

			rt, closeFn, err := selectRuntime(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if closeFn != nil {
				defer func() { _ = closeFn() }()
			}

			if err := verifyRunnerImage(cmd.Context(), rt, cfg.Runner.Image); err != nil {
				return err
			}
			logger.Info("doctor runner image ok", "image", cfg.Runner.Image)

			if err := verifyRunnerRuntime(cmd.Context(), rt, cfg); err != nil {
				return err
			}
			logger.Info("doctor runner runtime ok", "binary", cfg.Runner.Binary)

			keyStore, err := sshkeys.NewStoreWithLogger(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir, logger)
			if err != nil {
				return err
			}
			agentManager, err := sshagent.NewManagerWithLogger(keyStore, cfg.SSH.AgentDir, logger)
			if err != nil {
				return err
			}
			defer func() { _ = agentManager.Close() }()

			runnerProvider, err := runnercontainer.NewProvider(cmd.Context(), runnercontainer.Config{
				Image:          cfg.Runner.Image,
				RepoRoot:       cfg.RepoRoot,
				RunnerRepoRoot: cfg.Runner.RepoRoot,
				HostRepoRoot:   cfg.Runner.HostRepoRoot,
				HostStateDir:   cfg.Runner.HostStateDir,
				SockDir:        cfg.Runner.SockDir,
				StateDir:       cfg.StateDir,
				SkelData:       userhome.DefaultTemplateData(cfg),
				SSHAgentDir:    cfg.SSH.AgentDir,
				RunnerBinary:   cfg.Runner.Binary,
				RunnerArgs:     cfg.Runner.Args,
				RunnerEnv:      cfg.Runner.Env,
				GitSSHDebug:    cfg.Runner.GitSSHDebug,
				IdleTimeout:    0,
			}, rt, agentManager)
			if err != nil {
				return err
			}

			repoName := fmt.Sprintf("doctor-%d", time.Now().UnixNano())
			hostRepoPath := filepath.Join(cfg.RepoRoot, repoName)
			if err := os.MkdirAll(hostRepoPath, 0o755); err != nil {
				return fmt.Errorf("doctor repo: %w", err)
			}

			tabID := schema.TabID(fmt.Sprintf("doctor-%d", time.Now().UnixNano()))
			resp, err := runnerProvider.RunnerFor(cmd.Context(), core.RunnerRequest{
				UserID: schema.UserID(user),
				TabID:  tabID,
			})
			if err != nil {
				return err
			}
			defer func() {
				_ = runnerProvider.CloseTab(context.Background(), core.RunnerCloseRequest{
					UserID: schema.UserID(user),
					TabID:  tabID,
				})
			}()

			workDir := path.Join(resp.Info.RepoRoot, repoName)
			logger.Info("doctor runner ready", "user", user, "tab", tabID, "workdir", workDir)

			if err := runDoctorCommand(cmd.Context(), logger, resp.Runner, resp.Info.SSHAuthSock, workDir, "pwd", commandTimeout); err != nil {
				return err
			}
			if err := runDoctorCommand(cmd.Context(), logger, resp.Runner, resp.Info.SSHAuthSock, workDir, "git init -q", commandTimeout); err != nil {
				return err
			}
			if err := runDoctorCommand(cmd.Context(), logger, resp.Runner, resp.Info.SSHAuthSock, workDir, "git status --porcelain", commandTimeout); err != nil {
				return err
			}
			logger.Info("doctor command checks ok")

			if err := runDoctorCodex(cmd.Context(), logger, resp.Runner, resp.Info.SSHAuthSock, workDir, codexPrompt, schema.ModelID(cfg.Models.Default), codexTimeout); err != nil {
				return err
			}
			logger.Info("doctor complete")
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "path to config file")
	cmd.Flags().StringVar(&user, "user", "doctor", "runner username for diagnostics")
	cmd.Flags().StringVar(&codexPrompt, "codex-prompt", "Say 'ok' and exit.", "prompt used for codex exec test")
	cmd.Flags().DurationVar(&commandTimeout, "command-timeout", 15*time.Second, "timeout for command checks")
	cmd.Flags().DurationVar(&codexTimeout, "codex-timeout", 90*time.Second, "timeout for codex exec check")
	return cmd
}

func runDoctorCommand(ctx context.Context, logger pslog.Logger, runner core.Runner, sshSock, workDir, cmd string, timeout time.Duration) error {
	if strings.TrimSpace(cmd) == "" {
		return errors.New("doctor command is empty")
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	logger.Info("doctor command start", "command", cmd)
	handle, err := runner.RunCommand(runCtx, core.RunCommandRequest{
		WorkingDir:  workDir,
		Command:     cmd,
		UseShell:    true,
		SSHAuthSock: sshSock,
	})
	if err != nil {
		return fmt.Errorf("doctor command start (%s): %w", cmd, err)
	}
	outputs := handle.Outputs()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			out, err := outputs.Next(runCtx)
			if err != nil {
				return
			}
			if strings.TrimSpace(out.Text) == "" {
				continue
			}
			logger.Debug("doctor command output", "command", cmd, "stream", out.Stream, "text", out.Text)
		}
	}()
	result, err := handle.Wait(runCtx)
	<-done
	if err != nil {
		return fmt.Errorf("doctor command failed (%s): %w", cmd, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("doctor command failed (%s exit %d)", cmd, result.ExitCode)
	}
	logger.Info("doctor command ok", "command", cmd, "exit", result.ExitCode)
	return nil
}

func runDoctorCodex(ctx context.Context, logger pslog.Logger, runner core.Runner, sshSock, workDir, prompt string, modelID schema.ModelID, timeout time.Duration) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("doctor codex prompt is empty")
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	logger.Info("doctor codex start", "model", modelID)
	handle, err := runner.Run(runCtx, core.RunRequest{
		WorkingDir:           workDir,
		Prompt:               prompt,
		Model:                modelID,
		ModelReasoningEffort: schema.DefaultModelReasoningEffort,
		JSON:                 true,
		SSHAuthSock:          sshSock,
	})
	if err != nil {
		return fmt.Errorf("doctor codex start: %w", err)
	}
	events := handle.Events()
	done := make(chan struct{})
	var count int
	go func() {
		defer close(done)
		for {
			_, err := events.Next(runCtx)
			if err != nil {
				return
			}
			count++
		}
	}()
	result, err := handle.Wait(runCtx)
	<-done
	if err != nil {
		return fmt.Errorf("doctor codex failed: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("doctor codex failed (exit %d)", result.ExitCode)
	}
	logger.Info("doctor codex ok", "events", count, "exit", result.ExitCode)
	return nil
}
