package shipohoy

import "context"

// Runtime manages container lifecycles.
type Runtime interface {
	EnsureImage(ctx context.Context, image string) error
	EnsureRunning(ctx context.Context, spec ContainerSpec) (Handle, error)
	Stop(ctx context.Context, handle Handle) error
	Remove(ctx context.Context, handle Handle) error
	Exec(ctx context.Context, handle Handle, spec ExecSpec) (ExecResult, error)
	WaitForPort(ctx context.Context, handle Handle, spec WaitPortSpec) error
	WaitForLog(ctx context.Context, handle Handle, spec WaitLogSpec) error
	Janitor(ctx context.Context, spec JanitorSpec) (int, error)
}

// Builder builds container images.
type Builder interface {
	Build(ctx context.Context, spec BuildSpec) (BuildResult, error)
}

// BuilderWithEvents streams build progress events.
type BuilderWithEvents interface {
	BuildWithEvents(ctx context.Context, spec BuildSpec, events chan<- BuildEvent) (BuildResult, error)
}

// Handle represents a running container.
type Handle interface {
	Name() string
	ID() string
}
