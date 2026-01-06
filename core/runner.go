package core

import (
	"context"

	"pkt.systems/centaurx/schema"
)

// Runner starts codex exec processes and exposes their JSONL event stream.
type Runner interface {
	Run(ctx context.Context, req RunRequest) (RunHandle, error)
	RunCommand(ctx context.Context, req RunCommandRequest) (CommandHandle, error)
}

// RunRequest describes a codex exec invocation.
type RunRequest struct {
	WorkingDir      string
	Prompt          string
	Model           schema.ModelID
	ResumeSessionID schema.SessionID
	JSON            bool
	SSHAuthSock     string
}

// RunHandle exposes the event stream and process lifecycle controls.
type RunHandle interface {
	Events() EventStream
	Signal(ctx context.Context, sig ProcessSignal) error
	Wait(ctx context.Context) (RunResult, error)
	Close() error
}

// EventStream yields normalized events from codex exec.
type EventStream interface {
	Next(ctx context.Context) (schema.ExecEvent, error)
	Close() error
}

// RunResult describes the process outcome.
type RunResult struct {
	ExitCode int
}

// RunCommandRequest describes an arbitrary command invocation.
type RunCommandRequest struct {
	WorkingDir  string
	Command     string
	UseShell    bool
	SSHAuthSock string
}

// CommandStreamKind indicates which stream produced output.
type CommandStreamKind string

const (
	// CommandStreamStdout indicates output captured from stdout.
	CommandStreamStdout CommandStreamKind = "stdout"
	// CommandStreamStderr indicates output captured from stderr.
	CommandStreamStderr CommandStreamKind = "stderr"
)

// CommandOutput captures a line of output from a command.
type CommandOutput struct {
	Stream CommandStreamKind
	Text   string
}

// CommandStream yields command output lines.
type CommandStream interface {
	Next(ctx context.Context) (CommandOutput, error)
	Close() error
}

// CommandHandle exposes output and lifecycle controls for a command.
type CommandHandle interface {
	Outputs() CommandStream
	Signal(ctx context.Context, sig ProcessSignal) error
	Wait(ctx context.Context) (RunResult, error)
	Close() error
}

// ProcessSignal indicates which signal to send to the process.
type ProcessSignal string

const (
	// ProcessSignalHUP requests a hangup signal.
	ProcessSignalHUP ProcessSignal = "HUP"
	// ProcessSignalTERM requests a termination signal.
	ProcessSignalTERM ProcessSignal = "TERM"
	// ProcessSignalKILL requests an immediate kill signal.
	ProcessSignalKILL ProcessSignal = "KILL"
)
