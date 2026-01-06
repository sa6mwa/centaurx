package shipohoy

import (
	"context"
	"sync"

	"pkt.systems/pslog"
)

// Yard manages a set of containers using a runtime backend.
type Yard struct {
	runtime Runtime
	builder Builder
	plan    YardPlan

	mu         sync.Mutex
	handles    map[Container]Handle
	containers []Container
}

// Commission creates a new yard with the given plan.
func Commission(plan YardPlan, runtime Runtime, builder Builder) *Yard {
	return &Yard{
		runtime: runtime,
		builder: builder,
		plan:    plan,
		handles: make(map[Container]Handle),
	}
}

// GateIn registers a container with the yard.
func (y *Yard) GateIn(_ context.Context, c Container) Container {
	log := pslog.Ctx(context.Background())
	if c != nil {
		log = log.With("container", c.Name())
	}
	log.Debug("yard gate in")
	y.mu.Lock()
	defer y.mu.Unlock()
	y.containers = append(y.containers, c)
	return c
}

// ShipOut ensures the container is running.
func (y *Yard) ShipOut(ctx context.Context, c Container) (Handle, error) {
	log := pslog.Ctx(ctx)
	if c != nil {
		log = log.With("container", c.Name())
	}
	log.Info("yard ship out start")
	spec := mergeSpec(c.Spec(), y.plan)
	handle, err := y.runtime.EnsureRunning(ctx, spec)
	if err != nil {
		log.Warn("yard ship out failed", "err", err)
		return nil, err
	}
	y.mu.Lock()
	y.handles[c] = handle
	y.mu.Unlock()
	log.Info("yard ship out ok")
	return handle, nil
}

// Discharge stops and removes a container.
func (y *Yard) Discharge(ctx context.Context, c Container) error {
	log := pslog.Ctx(ctx)
	if c != nil {
		log = log.With("container", c.Name())
	}
	log.Info("yard discharge start")
	y.mu.Lock()
	handle := y.handles[c]
	delete(y.handles, c)
	y.mu.Unlock()
	if handle == nil {
		log.Info("yard discharge skipped", "reason", "no handle")
		return nil
	}
	if err := y.runtime.Stop(ctx, handle); err != nil {
		log.Warn("yard discharge stop failed", "err", err)
		return err
	}
	if err := y.runtime.Remove(ctx, handle); err != nil {
		log.Warn("yard discharge remove failed", "err", err)
		return err
	}
	log.Info("yard discharge ok")
	return nil
}

// ShipOutAll starts all gated containers.
func (y *Yard) ShipOutAll(ctx context.Context) error {
	log := pslog.Ctx(ctx)
	y.mu.Lock()
	containers := append([]Container(nil), y.containers...)
	y.mu.Unlock()
	log.Info("yard ship out all start", "count", len(containers))
	for _, c := range containers {
		if _, err := y.ShipOut(ctx, c); err != nil {
			log.Warn("yard ship out all failed", "err", err)
			return err
		}
	}
	log.Info("yard ship out all ok", "count", len(containers))
	return nil
}

// DischargeAll stops all containers.
func (y *Yard) DischargeAll(ctx context.Context) {
	log := pslog.Ctx(ctx)
	y.mu.Lock()
	containers := append([]Container(nil), y.containers...)
	y.mu.Unlock()
	log.Info("yard discharge all start", "count", len(containers))
	for _, c := range containers {
		_ = y.Discharge(ctx, c)
	}
	log.Info("yard discharge all ok", "count", len(containers))
}
