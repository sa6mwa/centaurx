package runnergrpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"pkt.systems/centaurx/core"
)

func TestWrapRunnerErrorUnauthenticated(t *testing.T) {
	wrapped := wrapRunnerError("exec", status.Error(codes.Unauthenticated, "no auth"))
	var runnerErr *core.RunnerError
	if !errors.As(wrapped, &runnerErr) {
		t.Fatalf("expected RunnerError, got %T", wrapped)
	}
	if runnerErr.Kind != core.RunnerErrorUnauthorized {
		t.Fatalf("expected unauthorized, got %s", runnerErr.Kind)
	}
}

func TestWrapRunnerErrorUnavailable(t *testing.T) {
	wrapped := wrapRunnerError("exec", status.Error(codes.Unavailable, "down"))
	var runnerErr *core.RunnerError
	if !errors.As(wrapped, &runnerErr) {
		t.Fatalf("expected RunnerError, got %T", wrapped)
	}
	if runnerErr.Kind != core.RunnerErrorUnavailable {
		t.Fatalf("expected unavailable, got %s", runnerErr.Kind)
	}
}

func TestWrapRunnerErrorCanceled(t *testing.T) {
	wrapped := wrapRunnerError("exec", context.Canceled)
	var runnerErr *core.RunnerError
	if !errors.As(wrapped, &runnerErr) {
		t.Fatalf("expected RunnerError, got %T", wrapped)
	}
	if runnerErr.Kind != core.RunnerErrorCanceled {
		t.Fatalf("expected canceled, got %s", runnerErr.Kind)
	}
}
