package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// Config controls how the codex exec runner is invoked.
type Config struct {
	BinaryPath string
	ExtraArgs  []string
	Env        []string
}

// Runner implements core.Runner.
type Runner struct {
	cfg Config
}

// NewRunner constructs a codex exec runner.
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codex"
	}
	return &Runner{cfg: cfg}, nil
}

// Run starts a codex exec process.
func (r *Runner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	if req.Prompt == "" {
		return nil, schema.ErrEmptyPrompt
	}
	args := buildExecArgs(r.cfg, req)
	log := pslog.Ctx(ctx)
	if log != nil {
		log.Info(
			"codex exec start",
			"workdir", req.WorkingDir,
			"args_len", len(args),
			"args", args,
			"model", req.Model,
			"resume", req.ResumeSessionID != "",
			"json", req.JSON,
			"prompt_len", len(req.Prompt),
			"ssh_auth_sock", req.SSHAuthSock != "",
			"env_extra", len(r.cfg.Env),
		)
	}

	cmd := exec.CommandContext(ctx, r.cfg.BinaryPath, args...)
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	if len(r.cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), r.cfg.Env...)
	} else {
		cmd.Env = append(cmd.Env, os.Environ()...)
	}
	if req.SSHAuthSock != "" {
		cmd.Env = append(filterEnv(cmd.Env, "SSH_AUTH_SOCK"), fmt.Sprintf("SSH_AUTH_SOCK=%s", req.SSHAuthSock))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if log != nil {
			log.Error("codex exec stdout failed", "err", err)
		}
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if log != nil {
			log.Error("codex exec stderr failed", "err", err)
		}
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		if log != nil {
			log.Error("codex exec stdin failed", "err", err)
		}
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		if log != nil {
			log.Error("codex exec start failed", "err", err)
		}
		return nil, err
	}
	if log != nil && cmd.Process != nil {
		log.Info("codex exec started", "pid", cmd.Process.Pid)
	}

	go func() {
		_, _ = io.WriteString(stdin, req.Prompt)
		_ = stdin.Close()
	}()

	stream := newCombinedStream(ctx, stdout, stderr)
	handle := &runHandle{
		cmd:     cmd,
		stream:  stream,
		log:     log,
		started: time.Now(),
	}
	return handle, nil
}

func buildExecArgs(cfg Config, req core.RunRequest) []string {
	args := []string{"exec"}
	if req.JSON {
		args = append(args, "--json")
	}
	if req.Model != "" {
		args = append(args, "--model", string(req.Model))
	}
	args = append(args, cfg.ExtraArgs...)
	if req.ResumeSessionID != "" {
		args = append(args, "resume", string(req.ResumeSessionID))
	}
	args = append(args, "-")
	return args
}

// RunCommand is not supported by the codex runner.
func (r *Runner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	_ = ctx
	_ = req
	return nil, errors.New("run command not supported")
}

type runHandle struct {
	cmd     *exec.Cmd
	stream  *combinedStream
	log     pslog.Logger
	started time.Time
}

func (r *runHandle) Events() core.EventStream {
	return r.stream
}

func (r *runHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_ = ctx
	if r.cmd == nil || r.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	switch sig {
	case core.ProcessSignalHUP:
		return r.cmd.Process.Signal(syscall.SIGHUP)
	case core.ProcessSignalTERM:
		return r.cmd.Process.Signal(syscall.SIGTERM)
	case core.ProcessSignalKILL:
		return r.cmd.Process.Signal(syscall.SIGKILL)
	default:
		return fmt.Errorf("unsupported signal: %s", sig)
	}
}

func (r *runHandle) Wait(ctx context.Context) (core.RunResult, error) {
	_ = ctx
	if r.cmd == nil {
		return core.RunResult{}, fmt.Errorf("process not started")
	}
	err := r.cmd.Wait()
	signal := ""
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				signal = status.Signal().String()
			}
		} else {
			if r.log != nil {
				r.log.Error("codex exec wait failed", "err", err)
			}
			return core.RunResult{}, err
		}
	}
	if r.log != nil {
		fields := []any{
			"exit_code", exitCode,
			"duration_ms", time.Since(r.started).Milliseconds(),
		}
		if signal != "" {
			fields = append(fields, "signal", signal)
		}
		if err != nil {
			fields = append(fields, "err", err)
		}
		r.log.Info("codex exec finished", fields...)
	}
	return core.RunResult{ExitCode: exitCode}, nil
}

func (r *runHandle) Close() error {
	if r.stream != nil {
		_ = r.stream.Close()
	}
	return nil
}

func filterEnv(env []string, key string) []string {
	if len(env) == 0 {
		return env
	}
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return out
}
