package buildkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"

	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/pslog"
)

// Config configures the BuildKit builder.
type Config struct {
	Address string
}

// Builder implements shipohoy.Builder using BuildKit.
type Builder struct {
	addresses []string
}

// New constructs a BuildKit builder with fallback socket addresses.
func New(cfg Config) *Builder {
	return &Builder{addresses: candidateAddresses(cfg.Address)}
}

// Build builds an image using BuildKit.
func (b *Builder) Build(ctx context.Context, spec shipohoy.BuildSpec) (shipohoy.BuildResult, error) {
	return b.build(ctx, spec, nil)
}

// BuildWithEvents builds an image and streams progress events.
func (b *Builder) BuildWithEvents(ctx context.Context, spec shipohoy.BuildSpec, events chan<- shipohoy.BuildEvent) (shipohoy.BuildResult, error) {
	return b.build(ctx, spec, events)
}

func (b *Builder) build(ctx context.Context, spec shipohoy.BuildSpec, events chan<- shipohoy.BuildEvent) (shipohoy.BuildResult, error) {
	log := pslog.Ctx(ctx).With("backend", "buildkit")
	if len(spec.Tags) == 0 {
		log.Warn("buildkit build rejected", "reason", "missing tags")
		return shipohoy.BuildResult{}, errors.New("build tags are required")
	}
	contextDir := spec.ContextDir
	dockerfilePath := spec.ContainerfilePath
	tempDir := ""
	if len(spec.ContainerfileData) > 0 {
		dir, err := os.MkdirTemp("", "centaurx-containerfile-*")
		if err != nil {
			return shipohoy.BuildResult{}, err
		}
		tempDir = dir
		dockerfilePath = filepath.Join(dir, "Containerfile")
		if err := os.WriteFile(dockerfilePath, spec.ContainerfileData, 0o600); err != nil {
			return shipohoy.BuildResult{}, err
		}
		if contextDir == "" {
			contextDir = dir
		}
	}
	if contextDir == "" {
		log.Warn("buildkit build rejected", "reason", "missing context")
		return shipohoy.BuildResult{}, errors.New("build context is required")
	}
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(contextDir, "Containerfile")
	}
	defer func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}()

	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	log.Info("buildkit build start", "tags", len(spec.Tags), "timeout_ms", timeout.Milliseconds())
	buildCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bkclient, err := b.dial(buildCtx)
	if err != nil {
		log.Warn("buildkit build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}
	defer func() { _ = bkclient.Close() }()

	dockerfileDir := filepath.Dir(dockerfilePath)
	attrs := map[string]string{
		"filename": filepath.Base(dockerfilePath),
	}
	for k, v := range spec.BuildArgs {
		attrs["build-arg:"+k] = v
	}

	var statusCh chan *client.SolveStatus
	var wg sync.WaitGroup
	if events != nil {
		statusCh = make(chan *client.SolveStatus)
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.emitEvents(buildCtx, statusCh, events)
		}()
	}

	exports, err := buildExports(spec)
	if err != nil {
		log.Warn("buildkit build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}

	_, err = bkclient.Solve(buildCtx, nil, client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: attrs,
		LocalDirs: map[string]string{
			"context":    contextDir,
			"dockerfile": dockerfileDir,
		},
		Exports: exports,
	}, statusCh)
	if statusCh != nil {
		wg.Wait()
	}
	if err != nil {
		log.Warn("buildkit build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}
	if strings.TrimSpace(spec.OutputPath) != "" {
		pslog.Ctx(ctx).Info("build.export.complete", "path", spec.OutputPath, "backend", "buildkit")
	}
	log.Info("buildkit build ok", "tags", len(spec.Tags))
	return shipohoy.BuildResult{ImageNames: spec.Tags}, nil
}

func (b *Builder) emitEvents(ctx context.Context, statusCh <-chan *client.SolveStatus, events chan<- shipohoy.BuildEvent) {
	type vertexState struct {
		name      string
		started   bool
		completed bool
		lastError string
	}
	vertices := make(map[string]*vertexState)
	for {
		select {
		case <-ctx.Done():
			return
		case status, ok := <-statusCh:
			if !ok {
				return
			}
			for _, v := range status.Vertexes {
				if v == nil {
					continue
				}
				id := v.Digest.String()
				state := vertices[id]
				if state == nil {
					state = &vertexState{name: v.Name}
					vertices[id] = state
				} else if state.name == "" && v.Name != "" {
					state.name = v.Name
				}
				if v.Started != nil && !state.started {
					state.started = true
					sendBuildEvent(ctx, events, shipohoy.BuildEvent{
						Kind:      shipohoy.BuildEventVertexStarted,
						VertexID:  id,
						Name:      state.name,
						Timestamp: *v.Started,
					})
				}
				if v.Completed != nil && !state.completed {
					state.completed = true
					state.lastError = v.Error
					sendBuildEvent(ctx, events, shipohoy.BuildEvent{
						Kind:      shipohoy.BuildEventVertexCompleted,
						VertexID:  id,
						Name:      state.name,
						Timestamp: *v.Completed,
						Error:     v.Error,
					})
				}
				if v.Error != "" && v.Error != state.lastError {
					state.lastError = v.Error
					sendBuildEvent(ctx, events, shipohoy.BuildEvent{
						Kind:     shipohoy.BuildEventVertexCompleted,
						VertexID: id,
						Name:     state.name,
						Error:    v.Error,
					})
				}
			}
			for _, log := range status.Logs {
				if log == nil {
					continue
				}
				msg := strings.TrimSpace(string(log.Data))
				if msg == "" {
					continue
				}
				name := ""
				if state := vertices[log.Vertex.String()]; state != nil {
					name = state.name
				}
				sendBuildEvent(ctx, events, shipohoy.BuildEvent{
					Kind:      shipohoy.BuildEventLog,
					VertexID:  log.Vertex.String(),
					Name:      name,
					Message:   msg,
					Timestamp: log.Timestamp,
				})
			}
			for _, warn := range status.Warnings {
				if warn == nil {
					continue
				}
				short := strings.TrimSpace(string(warn.Short))
				if warn.URL != "" {
					if short != "" {
						short = short + " (" + warn.URL + ")"
					} else {
						short = warn.URL
					}
				}
				if short == "" {
					continue
				}
				name := ""
				if state := vertices[warn.Vertex.String()]; state != nil {
					name = state.name
				}
				sendBuildEvent(ctx, events, shipohoy.BuildEvent{
					Kind:     shipohoy.BuildEventWarning,
					VertexID: warn.Vertex.String(),
					Name:     name,
					Message:  short,
				})
			}
		}
	}
}

func buildExports(spec shipohoy.BuildSpec) ([]client.ExportEntry, error) {
	if strings.TrimSpace(spec.OutputPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.OutputPath), 0o755); err != nil {
			return nil, err
		}
		logger := pslog.Ctx(context.Background())
		logger.Info("build.export.start", "path", spec.OutputPath, "backend", "buildkit")
		output := func(_ map[string]string) (io.WriteCloser, error) {
			return os.Create(spec.OutputPath)
		}
		return []client.ExportEntry{
			{
				Type:   client.ExporterOCI,
				Output: output,
				Attrs: map[string]string{
					"name":           strings.Join(spec.Tags, ","),
					"tar":            "true",
					"oci-mediatypes": "true",
				},
			},
		}, nil
	}
	return []client.ExportEntry{
		{
			Type: client.ExporterImage,
			Attrs: map[string]string{
				"name":           strings.Join(spec.Tags, ","),
				"push":           "false",
				"store":          "true",
				"unpack":         "true",
				"oci-mediatypes": "true",
			},
		},
	}, nil
}

func sendBuildEvent(ctx context.Context, events chan<- shipohoy.BuildEvent, event shipohoy.BuildEvent) {
	if events == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case events <- event:
	default:
	}
}

func (b *Builder) dial(ctx context.Context) (*client.Client, error) {
	var lastErr error
	for _, addr := range b.addresses {
		c, err := client.New(ctx, addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("buildkit address not configured")
	}
	return nil, lastErr
}

func candidateAddresses(primary string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(addr string) {
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	add(primary)

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir != "" {
		add(fmt.Sprintf("unix://%s", filepath.Join(runtimeDir, "buildkit", "buildkitd.sock")))
	}
	userRunDir := filepath.Join("/run", "user", fmt.Sprintf("%d", os.Getuid()))
	if userRunDir != runtimeDir {
		add(fmt.Sprintf("unix://%s", filepath.Join(userRunDir, "buildkit", "buildkitd.sock")))
	}
	add("unix:///run/buildkit/buildkitd.sock")
	return out
}
