package core

import "fmt"

// RunnerErrorKind classifies runner failures for user-facing hints.
type RunnerErrorKind string

const (
	// RunnerErrorUnknown is an uncategorized runner failure.
	RunnerErrorUnknown RunnerErrorKind = "unknown"
	// RunnerErrorUnavailable indicates the runner is unreachable.
	RunnerErrorUnavailable RunnerErrorKind = "unavailable"
	// RunnerErrorUnauthorized indicates authentication failed.
	RunnerErrorUnauthorized RunnerErrorKind = "unauthorized"
	// RunnerErrorPermissionDenied indicates authorization failed.
	RunnerErrorPermissionDenied RunnerErrorKind = "permission_denied"
	// RunnerErrorTimeout indicates the runner timed out.
	RunnerErrorTimeout RunnerErrorKind = "timeout"
	// RunnerErrorCanceled indicates the runner request was canceled.
	RunnerErrorCanceled RunnerErrorKind = "canceled"
	// RunnerErrorContainerStart indicates the container failed to start.
	RunnerErrorContainerStart RunnerErrorKind = "container_start"
	// RunnerErrorContainerSocket indicates the runner socket did not appear.
	RunnerErrorContainerSocket RunnerErrorKind = "container_socket"
	// RunnerErrorExec indicates an exec request failed.
	RunnerErrorExec RunnerErrorKind = "exec"
	// RunnerErrorCommand indicates a command request failed.
	RunnerErrorCommand RunnerErrorKind = "command"
)

// RunnerError wraps runner failures with a stable classification.
type RunnerError struct {
	Kind    RunnerErrorKind
	Op      string
	Message string
	Err     error
}

// NewRunnerError constructs a classified runner error.
func NewRunnerError(kind RunnerErrorKind, op string, err error) *RunnerError {
	return &RunnerError{Kind: kind, Op: op, Err: err}
}

func (e *RunnerError) Error() string {
	if e == nil {
		return "runner error"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Op != "" {
		return fmt.Sprintf("runner %s failed", e.Op)
	}
	return "runner error"
}

func (e *RunnerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
