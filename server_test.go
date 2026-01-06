package centaurx

import (
	"context"
	"errors"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
)

func TestServerStopClosesRunners(t *testing.T) {
	runners := &trackingRunnerProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	server := &compositeServer{
		runners: runners,
		ctx:     ctx,
		cancel:  cancel,
		started: true,
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := server.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runners.closed != 1 {
		t.Fatalf("expected CloseAll to be called, got %d", runners.closed)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("expected server context to be canceled")
	}
}

type trackingRunnerProvider struct {
	closed int
}

func (t *trackingRunnerProvider) RunnerFor(context.Context, core.RunnerRequest) (core.RunnerResponse, error) {
	return core.RunnerResponse{}, errors.New("not implemented")
}

func (t *trackingRunnerProvider) CloseTab(context.Context, core.RunnerCloseRequest) error {
	return nil
}

func (t *trackingRunnerProvider) CloseAll(context.Context) error {
	t.closed++
	return nil
}
