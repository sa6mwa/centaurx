package core

import (
	"context"
	"fmt"

	"pkt.systems/centaurx/schema"
)

// RunnerInfo describes runner-specific context for a user.
type RunnerInfo struct {
	RepoRoot    string
	HomeDir     string
	SSHAuthSock string
}

// RunnerRequest selects a runner instance.
type RunnerRequest struct {
	UserID schema.UserID
	TabID  schema.TabID
}

// RunnerResponse returns a runner plus context.
type RunnerResponse struct {
	Runner Runner
	Info   RunnerInfo
}

// RunnerCloseRequest identifies a tab to close.
type RunnerCloseRequest struct {
	UserID schema.UserID
	TabID  schema.TabID
}

// RunnerProvider returns a runner for a given user.
type RunnerProvider interface {
	RunnerFor(ctx context.Context, req RunnerRequest) (RunnerResponse, error)
	CloseTab(ctx context.Context, req RunnerCloseRequest) error
	CloseAll(ctx context.Context) error
}

// StaticRunnerProvider wraps a single runner instance for all users.
type StaticRunnerProvider struct {
	Runner Runner
}

// RunnerFor returns the configured runner.
func (p StaticRunnerProvider) RunnerFor(_ context.Context, _ RunnerRequest) (RunnerResponse, error) {
	if p.Runner == nil {
		return RunnerResponse{}, fmt.Errorf("runner provider has no runner")
	}
	return RunnerResponse{Runner: p.Runner}, nil
}

// CloseTab is a no-op for the static provider.
func (p StaticRunnerProvider) CloseTab(_ context.Context, _ RunnerCloseRequest) error {
	return nil
}

// CloseAll is a no-op for the static provider.
func (p StaticRunnerProvider) CloseAll(_ context.Context) error {
	return nil
}
