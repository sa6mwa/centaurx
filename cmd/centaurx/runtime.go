package main

import (
	"context"
	"fmt"
	"time"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/shipohoy/containerd"
	"pkt.systems/centaurx/internal/shipohoy/podman"
)

func selectRuntime(ctx context.Context, cfg appconfig.Config) (shipohoy.Runtime, func() error, error) {
	switch cfg.Runner.Runtime {
	case "podman":
		rt, err := podman.New(ctx, podman.Config{
			Address:     cfg.Runner.Podman.Address,
			UserNSMode:  cfg.Runner.Podman.UserNSMode,
			PullTimeout: time.Duration(cfg.Runner.PullTimeout) * time.Minute,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("podman connection failed (%s): %w", cfg.Runner.Podman.Address, err)
		}
		return rt, rt.Close, nil
	case "containerd":
		rt, err := containerd.New(ctx, containerd.Config{
			Address:     cfg.Runner.Containerd.Address,
			Namespace:   cfg.Runner.Containerd.Namespace,
			PullTimeout: time.Duration(cfg.Runner.PullTimeout) * time.Minute,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("containerd connection failed (%s): %w", cfg.Runner.Containerd.Address, err)
		}
		return rt, rt.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported runner.runtime %q", cfg.Runner.Runtime)
	}
}
