package core

import "context"

// RunnerServer exposes a runner service (gRPC over UDS).
type RunnerServer interface {
	ListenAndServe(ctx context.Context) error
}
