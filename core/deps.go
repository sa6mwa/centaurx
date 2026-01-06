package core

import "pkt.systems/pslog"

// ServiceDeps captures optional dependencies for the core service.
type ServiceDeps struct {
	RunnerProvider RunnerProvider
	RepoResolver   RepoResolver
	Renderer       Renderer
	EventSink      EventSink
	Logger         pslog.Logger
}
