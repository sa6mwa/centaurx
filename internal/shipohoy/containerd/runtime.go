package containerd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/core/images"
	transferimage "github.com/containerd/containerd/v2/core/transfer/image"
	"github.com/containerd/containerd/v2/core/transfer/registry"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"

	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/pslog"
)

// Config configures the containerd runtime.
type Config struct {
	Address     string
	Namespace   string
	PullTimeout time.Duration
}

// Runtime implements shipohoy.Runtime using containerd.
type Runtime struct {
	client      *containerd.Client
	namespace   string
	pullTimeout time.Duration

	logsMu   sync.Mutex
	logs     map[string]*logCapture
	watchMu  sync.Mutex
	watchers map[string]struct{}
}

// New constructs a containerd runtime, trying fallback socket paths if needed.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	log := pslog.Ctx(ctx).With("runtime", "containerd")
	addresses := candidateAddresses(cfg.Address, "containerd")
	var lastErr error
	for _, addr := range addresses {
		log.Debug("containerd connect attempt", "address", addr)
		client, err := containerd.New(addr)
		if err == nil {
			namespace := cfg.Namespace
			if namespace == "" {
				namespace = "centaurx"
			}
			timeout := cfg.PullTimeout
			if timeout == 0 {
				timeout = 5 * time.Minute
			}
			log.Info("containerd runtime ready", "address", addr, "namespace", namespace)
			return &Runtime{
				client:      client,
				namespace:   namespace,
				pullTimeout: timeout,
				logs:        make(map[string]*logCapture),
				watchers:    make(map[string]struct{}),
			}, nil
		}
		log.Warn("containerd connect failed", "address", addr, "err", err)
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("containerd address not configured")
	}
	log.Warn("containerd runtime unavailable", "err", lastErr)
	return nil, lastErr
}

// Close releases the containerd client.
func (r *Runtime) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	err := r.client.Close()
	r.logger(context.Background()).Info("containerd runtime closed")
	return err
}

// ImageExists reports whether an image exists locally without pulling.
func (r *Runtime) ImageExists(ctx context.Context, image string) (bool, error) {
	if strings.TrimSpace(image) == "" {
		r.logger(ctx).Warn("containerd image check rejected", "reason", "missing image")
		return false, errors.New("image is required")
	}
	log := r.logger(ctx).With("image", image)
	log.Debug("containerd image check")
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	if _, err := r.client.GetImage(ctx, image); err == nil {
		log.Debug("containerd image present")
		return true, nil
	} else if errdefs.IsNotFound(err) {
		log.Debug("containerd image missing")
		return false, nil
	} else {
		log.Warn("containerd image check failed", "err", err)
		return false, err
	}
}

// Import loads an OCI tar image into the containerd image store.
func (r *Runtime) Import(ctx context.Context, tarPath string, tags []string) error {
	if strings.TrimSpace(tarPath) == "" {
		return errors.New("tar path is required")
	}
	log := r.logger(ctx).With("tar", tarPath)
	log.Info("containerd import start", "tags", len(tags))
	file, err := os.Open(tarPath)
	if err != nil {
		log.Warn("containerd import failed", "err", err)
		return err
	}
	defer func() { _ = file.Close() }()

	ctx = namespaces.WithNamespace(ctx, r.namespace)
	imported, err := r.client.Import(ctx, file)
	if err != nil {
		log.Warn("containerd import failed", "err", err)
		return err
	}
	if len(tags) == 0 {
		log.Info("containerd import ok", "images", len(imported))
		return nil
	}
	if len(imported) == 0 {
		log.Warn("containerd import failed", "err", "import did not return any images")
		return errors.New("import did not return any images")
	}
	existing := map[string]struct{}{}
	for _, img := range imported {
		if strings.TrimSpace(img.Name) == "" {
			continue
		}
		existing[img.Name] = struct{}{}
	}
	baseTarget := imported[0].Target
	if first := strings.TrimSpace(tags[0]); first != "" {
		if img, err := r.client.GetImage(ctx, first); err == nil {
			baseTarget = img.Target()
		}
	}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := existing[tag]; ok {
			continue
		}
		if _, err := r.client.GetImage(ctx, tag); err == nil {
			continue
		} else if !errdefs.IsNotFound(err) {
			return err
		}
		if err := r.tagImage(ctx, tag, baseTarget); err != nil {
			log.Warn("containerd import tag failed", "err", err, "tag", tag)
			return err
		}
	}
	log.Info("containerd import ok", "images", len(imported))
	return nil
}

func (r *Runtime) tagImage(ctx context.Context, name string, target ocispec.Descriptor) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if _, err := r.client.GetImage(ctx, name); err == nil {
		_, err = r.client.ImageService().Update(ctx, images.Image{Name: name, Target: target}, "target")
		return err
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	_, err := r.client.ImageService().Create(ctx, images.Image{Name: name, Target: target})
	return err
}

// EnsureImage pulls the image if it is not available.
func (r *Runtime) EnsureImage(ctx context.Context, image string) error {
	log := r.logger(ctx).With("image", image)
	log.Info("containerd ensure image start")
	_, err := r.ensureImage(ctx, image, "")
	if err != nil {
		log.Warn("containerd ensure image failed", "err", err)
		return err
	}
	log.Info("containerd ensure image ok")
	return nil
}

func (r *Runtime) ensureImage(ctx context.Context, image, snapshotter string) (containerd.Image, error) {
	if strings.TrimSpace(image) == "" {
		r.logger(ctx).Warn("containerd ensure image rejected", "reason", "missing image")
		return nil, errors.New("image is required")
	}
	log := r.logger(ctx).With("image", image)
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	rootless := os.Geteuid() != 0
	img, err := r.client.GetImage(ctx, image)
	if err == nil {
		log.Debug("containerd image present")
		if snapshotter != "" && !rootless {
			if err := img.Unpack(ctx, snapshotter); err != nil && !errdefs.IsAlreadyExists(err) {
				log.Warn("containerd image unpack failed", "err", err)
				return nil, err
			}
		}
		return img, nil
	}
	if !errdefs.IsNotFound(err) {
		log.Warn("containerd image lookup failed", "err", err)
		return nil, err
	}
	pullCtx, cancel := context.WithTimeout(ctx, r.pullTimeout)
	defer cancel()
	log.Info("containerd image pull start", "rootless", rootless)
	if pulled, err := r.pullWithTransfer(pullCtx, image, snapshotter, !rootless); err == nil {
		log.Info("containerd image pull ok", "method", "transfer")
		return pulled, nil
	} else if rootless {
		log.Warn("containerd transfer pull failed", "err", err)
		return nil, fmt.Errorf("transfer pull failed: %w", err)
	}
	opts := []containerd.RemoteOpt{containerd.WithPullUnpack}
	if snapshotter != "" {
		opts = append(opts, containerd.WithPullSnapshotter(snapshotter))
	}
	img, err = r.client.Pull(pullCtx, image, opts...)
	if err != nil {
		log.Warn("containerd image pull failed", "err", err)
		return nil, err
	}
	log.Info("containerd image pull ok", "method", "pull")
	return img, nil
}

func (r *Runtime) pullWithTransfer(ctx context.Context, image, snapshotter string, unpack bool) (containerd.Image, error) {
	storeOpts := []transferimage.StoreOpt{}
	if unpack {
		platform := platforms.DefaultSpec()
		storeOpts = append(storeOpts, transferimage.WithUnpack(platform, snapshotter))
	}
	store := transferimage.NewStore(image, storeOpts...)
	reg, err := registry.NewOCIRegistry(ctx, image)
	if err != nil {
		return nil, err
	}
	if err := r.client.Transfer(ctx, reg, store); err != nil {
		return nil, err
	}
	return r.client.GetImage(ctx, image)
}

// EnsureRunning ensures a container exists and is running.
func (r *Runtime) EnsureRunning(ctx context.Context, spec shipohoy.ContainerSpec) (shipohoy.Handle, error) {
	if strings.TrimSpace(spec.Name) == "" {
		r.logger(ctx).Warn("containerd ensure running rejected", "reason", "missing name")
		return nil, errors.New("container name is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		r.logger(ctx).Warn("containerd ensure running rejected", "reason", "missing image")
		return nil, errors.New("container image is required")
	}
	log := r.logger(ctx).With("container", spec.Name, "image", spec.Image)
	log.Info("containerd ensure running start")
	ctx = namespaces.WithNamespace(ctx, r.namespace)

	labels := mergeLabels(spec.Labels, map[string]string{
		labelManaged: "true",
	})

	container, err := r.client.LoadContainer(ctx, spec.Name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			log.Warn("containerd load container failed", "err", err)
			return nil, err
		}
		image, err := r.ensureImage(ctx, spec.Image, spec.Snapshotter)
		if err != nil {
			log.Warn("containerd ensure image failed", "err", err)
			return nil, err
		}
		specOpts := append([]oci.SpecOpts{oci.WithImageConfig(image)}, r.specOptions(spec)...)
		containerOpts := []containerd.NewContainerOpts{
			containerd.WithImage(image),
			containerd.WithContainerLabels(labels),
		}
		if strings.TrimSpace(spec.Snapshotter) != "" {
			containerOpts = append(containerOpts, containerd.WithSnapshotter(spec.Snapshotter))
		}
		containerOpts = append(containerOpts,
			containerd.WithNewSnapshot(spec.Name+"-snapshot", image),
			containerd.WithNewSpec(specOpts...),
		)
		container, err = r.client.NewContainer(ctx, spec.Name, containerOpts...)
		if err != nil {
			log.Warn("containerd create container failed", "err", err)
			return nil, err
		}
		log.Info("containerd container created", "id", container.ID())
	}

	logs := r.ensureLogCapture(spec.Name, spec.LogBufferBytes)
	stdoutWriter := logs.stdout
	stderrWriter := logs.stderr

	task, err := container.Task(ctx, nil)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			log.Warn("containerd task lookup failed", "err", err)
			return nil, err
		}
		task, err = container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, stdoutWriter, stderrWriter)))
		if err != nil {
			log.Warn("containerd task create failed", "err", err)
			return nil, err
		}
		if err := task.Start(ctx); err != nil {
			log.Warn("containerd task start failed", "err", err)
			_, _ = task.Delete(ctx)
			return nil, err
		}
		log.Info("containerd task started", "id", task.ID())
		logs.attached = true
	} else {
		status, err := task.Status(ctx)
		if err != nil {
			log.Warn("containerd task status failed", "err", err)
			return nil, err
		}
		if status.Status != containerd.Running {
			if err := task.Start(ctx); err != nil {
				log.Warn("containerd task start failed", "err", err)
				return nil, err
			}
			log.Info("containerd task started", "id", task.ID())
		}
		if !logs.attached {
			if _, attachErr := container.Task(ctx, cio.NewAttach(cio.WithStreams(nil, stdoutWriter, stderrWriter))); attachErr == nil {
				logs.attached = true
			}
		}
	}

	if spec.AutoRemove {
		r.watchAutoRemove(container, task, spec.Name)
	}
	log.Info("containerd container ready", "id", container.ID())
	return &handle{name: spec.Name, id: container.ID()}, nil
}

// Stop stops a running container task.
func (r *Runtime) Stop(ctx context.Context, handle shipohoy.Handle) error {
	if handle == nil {
		return nil
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Info("containerd stop start")
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	container, err := r.client.LoadContainer(ctx, handle.Name())
	if err != nil {
		if errdefs.IsNotFound(err) {
			log.Info("containerd stop skipped", "reason", "not found")
			return nil
		}
		log.Warn("containerd stop failed", "err", err)
		return err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			log.Info("containerd stop skipped", "reason", "task not found")
			return nil
		}
		log.Warn("containerd stop failed", "err", err)
		return err
	}
	_ = task.Kill(ctx, syscall.SIGTERM)
	_, _ = task.Delete(ctx)
	log.Info("containerd stop ok")
	return nil
}

// Remove deletes the container and its snapshot.
func (r *Runtime) Remove(ctx context.Context, handle shipohoy.Handle) error {
	if handle == nil {
		return nil
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Info("containerd remove start")
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	container, err := r.client.LoadContainer(ctx, handle.Name())
	if err != nil {
		if errdefs.IsNotFound(err) {
			log.Info("containerd remove skipped", "reason", "not found")
			return nil
		}
		log.Warn("containerd remove failed", "err", err)
		return err
	}
	err = container.Delete(ctx, containerd.WithSnapshotCleanup)
	r.clearLogCapture(handle.Name())
	if err != nil {
		log.Warn("containerd remove failed", "err", err)
		return err
	}
	log.Info("containerd remove ok")
	return nil
}

// Exec runs a command inside a running container.
func (r *Runtime) Exec(ctx context.Context, handle shipohoy.Handle, spec shipohoy.ExecSpec) (shipohoy.ExecResult, error) {
	if handle == nil {
		r.logger(ctx).Warn("containerd exec rejected", "reason", "missing handle")
		return shipohoy.ExecResult{}, errors.New("container handle is required")
	}
	if len(spec.Command) == 0 {
		r.logger(ctx).Warn("containerd exec rejected", "reason", "missing command")
		return shipohoy.ExecResult{}, errors.New("exec command is required")
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID(), "cmd_len", len(spec.Command))
	log.Info("containerd exec start")
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ctx = namespaces.WithNamespace(execCtx, r.namespace)
	container, err := r.client.LoadContainer(ctx, handle.Name())
	if err != nil {
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}

	proc, err := r.processSpec(ctx, container, spec)
	if err != nil {
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}

	stdout := spec.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := spec.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	execID := fmt.Sprintf("exec-%d", time.Now().UnixNano())
	creator := cio.NewCreator(cio.WithStreams(spec.Stdin, stdout, stderr))
	started := time.Now()
	process, err := task.Exec(ctx, execID, proc, creator)
	if err != nil {
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	if err := process.Start(ctx); err != nil {
		_, _ = process.Delete(ctx)
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	waitCh, err := process.Wait(ctx)
	if err != nil {
		_, _ = process.Delete(ctx)
		log.Warn("containerd exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}

	select {
	case status := <-waitCh:
		code, _, err := status.Result()
		finished := time.Now()
		_, _ = process.Delete(ctx)
		if err != nil {
			log.Warn("containerd exec failed", "err", err)
			return shipohoy.ExecResult{}, err
		}
		log.Info("containerd exec ok", "exit_code", int(code), "duration_ms", finished.Sub(started).Milliseconds())
		return shipohoy.ExecResult{ExitCode: int(code), Started: started, Finished: finished}, nil
	case <-ctx.Done():
		_ = process.Kill(context.Background(), syscall.SIGTERM)
		_, _ = process.Delete(context.Background())
		log.Warn("containerd exec timeout", "err", ctx.Err())
		return shipohoy.ExecResult{}, ctx.Err()
	}
}

// WaitForPort waits for a TCP port to accept connections.
func (r *Runtime) WaitForPort(ctx context.Context, handle shipohoy.Handle, spec shipohoy.WaitPortSpec) error {
	if handle == nil {
		r.logger(ctx).Warn("containerd wait for port rejected", "reason", "missing handle")
		return errors.New("container handle is required")
	}
	address := strings.TrimSpace(spec.Address)
	if address == "" {
		address = "127.0.0.1"
	}
	if spec.Port <= 0 {
		r.logger(ctx).Warn("containerd wait for port rejected", "reason", "invalid port", "port", spec.Port)
		return errors.New("port must be greater than zero")
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	interval := spec.Interval
	if interval == 0 {
		interval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("%s:%d", address, spec.Port)
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID(), "target", addr)
	log.Debug("containerd wait for port start", "timeout_ms", timeout.Milliseconds())

	for time.Now().Before(deadline) {
		dialer := net.Dialer{Timeout: interval}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			log.Debug("containerd wait for port ok")
			return nil
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if spec.NetNSFallback {
		if err := r.waitForPortNetNS(ctx, handle, addr); err == nil {
			return nil
		}
	}
	log.Warn("containerd wait for port failed", "timeout", timeout.String())
	return fmt.Errorf("port %s did not open within %s", addr, timeout)
}

// WaitForLog waits for a substring to appear in captured logs.
func (r *Runtime) WaitForLog(ctx context.Context, handle shipohoy.Handle, spec shipohoy.WaitLogSpec) error {
	if handle == nil {
		r.logger(ctx).Warn("containerd wait for log rejected", "reason", "missing handle")
		return errors.New("container handle is required")
	}
	text := spec.Text
	if text == "" {
		r.logger(ctx).Warn("containerd wait for log rejected", "reason", "missing text")
		return nil
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	interval := spec.Interval
	if interval == 0 {
		interval = 200 * time.Millisecond
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Debug("containerd wait for log start", "timeout_ms", timeout.Milliseconds())
	capture := r.getLogCapture(handle.Name())
	if capture == nil {
		log.Warn("containerd wait for log failed", "reason", "log capture unavailable")
		return errors.New("log capture unavailable")
	}
	want := []byte(text)
	deadline := time.Now().Add(timeout)
	for {
		if capture.contains(spec.Stream, want) {
			log.Debug("containerd wait for log ok")
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	log.Warn("containerd wait for log failed", "timeout", timeout.String())
	return fmt.Errorf("log text not found within %s", timeout)
}

// TailLogs returns the last N log lines captured for a container.
func (r *Runtime) TailLogs(ctx context.Context, handle shipohoy.Handle, limit int) ([]string, []string, error) {
	if handle == nil {
		return nil, nil, errors.New("container handle is required")
	}
	if limit <= 0 {
		limit = 50
	}
	capture := r.getLogCapture(handle.Name())
	if capture == nil {
		return nil, nil, errors.New("log capture unavailable")
	}
	stdout := tailLines(capture.stdout.Snapshot(), limit)
	stderr := tailLines(capture.stderr.Snapshot(), limit)
	return stdout, stderr, nil
}

// Janitor stops and removes managed containers.
func (r *Runtime) Janitor(ctx context.Context, spec shipohoy.JanitorSpec) (int, error) {
	log := r.logger(ctx)
	log.Info("containerd janitor start")
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	containers, err := r.client.Containers(ctx)
	if err != nil {
		log.Warn("containerd janitor failed", "err", err)
		return 0, err
	}
	removed := 0
	now := time.Now()
	for _, container := range containers {
		info, err := container.Info(ctx)
		if err != nil {
			continue
		}
		if !matchesLabels(info.Labels, spec.LabelSelector) {
			continue
		}
		if info.Labels[labelManaged] != "true" {
			continue
		}
		if spec.MinAge > 0 && now.Sub(info.CreatedAt) < spec.MinAge {
			continue
		}
		handle := &handle{name: info.ID, id: info.ID}
		_ = r.Stop(ctx, handle)
		if err := r.Remove(ctx, handle); err == nil {
			removed++
		}
	}
	log.Info("containerd janitor ok", "removed", removed)
	return removed, nil
}

func (r *Runtime) specOptions(spec shipohoy.ContainerSpec) []oci.SpecOpts {
	opts := []oci.SpecOpts{}
	opts = append(opts, oci.WithEnv(flattenEnv(spec.Env)))
	if spec.WorkingDir != "" {
		opts = append(opts, oci.WithProcessCwd(spec.WorkingDir))
	}
	if len(spec.Command) > 0 {
		opts = append(opts, oci.WithProcessArgs(spec.Command...))
	}
	if len(spec.Mounts) > 0 || len(spec.Tmpfs) > 0 {
		opts = append(opts, oci.WithMounts(mapMounts(spec.Mounts, spec.Tmpfs)))
	}
	if spec.ReadOnlyRootfs {
		opts = append(opts, oci.WithRootFSReadonly())
	}
	if spec.HostNetwork {
		opts = append(opts, oci.WithHostNamespace(specs.NetworkNamespace))
	}
	if spec.ResourceCaps != nil {
		opts = append(opts, withResources(*spec.ResourceCaps))
	}
	return opts
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

func mergeLabels(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func matchesLabels(labels map[string]string, selector map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func (r *Runtime) processSpec(ctx context.Context, container containerd.Container, spec shipohoy.ExecSpec) (*specs.Process, error) {
	baseSpec, err := container.Spec(ctx)
	if err != nil {
		return nil, err
	}
	proc := baseSpec.Process
	if proc == nil {
		proc = &specs.Process{}
	}
	proc = &specs.Process{
		Args:     spec.Command,
		Cwd:      proc.Cwd,
		Env:      mergeEnv(proc.Env, spec.Env),
		User:     proc.User,
		Terminal: false,
	}
	if spec.WorkingDir != "" {
		proc.Cwd = spec.WorkingDir
	}
	return proc, nil
}

func mergeEnv(base []string, add map[string]string) []string {
	if len(add) == 0 {
		return base
	}
	outMap := map[string]string{}
	for _, entry := range base {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			outMap[parts[0]] = parts[1]
		}
	}
	for k, v := range add {
		outMap[k] = v
	}
	out := make([]string, 0, len(outMap))
	for k, v := range outMap {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

func (r *Runtime) ensureLogCapture(name string, size int) *logCapture {
	if size <= 0 {
		size = defaultLogBufferBytes
	}
	r.logsMu.Lock()
	defer r.logsMu.Unlock()
	if capture, ok := r.logs[name]; ok {
		return capture
	}
	capture := &logCapture{
		stdout: newRingBuffer(size),
		stderr: newRingBuffer(size),
	}
	r.logs[name] = capture
	return capture
}

func (r *Runtime) getLogCapture(name string) *logCapture {
	r.logsMu.Lock()
	defer r.logsMu.Unlock()
	return r.logs[name]
}

func (r *Runtime) clearLogCapture(name string) {
	r.logsMu.Lock()
	defer r.logsMu.Unlock()
	delete(r.logs, name)
}

func (r *Runtime) watchAutoRemove(container containerd.Container, task containerd.Task, name string) {
	if name == "" {
		return
	}
	r.watchMu.Lock()
	if _, ok := r.watchers[name]; ok {
		r.watchMu.Unlock()
		return
	}
	r.watchers[name] = struct{}{}
	r.watchMu.Unlock()

	go func() {
		defer func() {
			r.watchMu.Lock()
			delete(r.watchers, name)
			r.watchMu.Unlock()
		}()
		ctx := namespaces.WithNamespace(context.Background(), r.namespace)
		statusCh, err := task.Wait(ctx)
		if err == nil {
			select {
			case <-statusCh:
			case <-ctx.Done():
				return
			}
		}
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		r.clearLogCapture(name)
	}()
}

func (r *Runtime) waitForPortNetNS(ctx context.Context, handle shipohoy.Handle, addr string) error {
	if runtime.GOOS != "linux" {
		return errors.New("netns fallback is only supported on linux")
	}
	ctx = namespaces.WithNamespace(ctx, r.namespace)
	container, err := r.client.LoadContainer(ctx, handle.Name())
	if err != nil {
		return err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		return err
	}
	pid := task.Pid()
	if pid == 0 {
		return errors.New("container pid unavailable")
	}
	origNS, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return err
	}
	defer func() { _ = origNS.Close() }()

	targetNS, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return err
	}
	defer func() { _ = targetNS.Close() }()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := unix.Setns(int(targetNS.Fd()), unix.CLONE_NEWNET); err != nil {
		return err
	}
	defer func() {
		_ = unix.Setns(int(origNS.Fd()), unix.CLONE_NEWNET)
	}()

	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func mapMounts(mounts []shipohoy.Mount, tmpfs []shipohoy.TmpfsMount) []specs.Mount {
	out := make([]specs.Mount, 0, len(mounts)+len(tmpfs))
	for _, mount := range mounts {
		opts := []string{"rbind"}
		if mount.ReadOnly {
			opts = append(opts, "ro")
		} else {
			opts = append(opts, "rw")
		}
		if mount.Propagation != "" {
			opts = append(opts, mount.Propagation)
		}
		out = append(out, specs.Mount{
			Type:        "bind",
			Source:      mount.Source,
			Destination: mount.Target,
			Options:     opts,
		})
	}
	for _, mount := range tmpfs {
		if strings.TrimSpace(mount.Target) == "" {
			continue
		}
		opts := append([]string{}, mount.Options...)
		if len(opts) == 0 {
			opts = []string{"rw", "nosuid", "nodev"}
		}
		out = append(out, specs.Mount{
			Type:        "tmpfs",
			Source:      "tmpfs",
			Destination: mount.Target,
			Options:     opts,
		})
	}
	return out
}

func withResources(caps shipohoy.ResourceCaps) oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, spec *specs.Spec) error {
		if spec.Linux == nil {
			spec.Linux = &specs.Linux{}
		}
		if spec.Linux.Resources == nil {
			spec.Linux.Resources = &specs.LinuxResources{}
		}
		if caps.MemoryBytes > 0 {
			spec.Linux.Resources.Memory = &specs.LinuxMemory{Limit: &caps.MemoryBytes}
		}
		if caps.NanoCPUs > 0 {
			period := uint64(100000)
			quota := int64(caps.NanoCPUs) * int64(period) / 1_000_000_000
			spec.Linux.Resources.CPU = &specs.LinuxCPU{Period: &period, Quota: &quota}
		}
		return nil
	}
}

func candidateAddresses(primary string, name string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(addr string) {
		addr = normalizeAddress(addr)
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
		add(filepath.Join(runtimeDir, name, name+".sock"))
	}
	userRunDir := filepath.Join("/run", "user", fmt.Sprintf("%d", os.Getuid()))
	if userRunDir != runtimeDir {
		add(filepath.Join(userRunDir, name, name+".sock"))
	}
	add(filepath.Join("/run", name, name+".sock"))
	return out
}

func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "unix://") {
		addr = strings.TrimPrefix(addr, "unix://")
	}
	if strings.HasPrefix(addr, "unix:") {
		addr = strings.TrimPrefix(addr, "unix:")
	}
	return addr
}

type handle struct {
	name string
	id   string
}

func (h *handle) Name() string { return h.name }
func (h *handle) ID() string   { return h.id }

const (
	labelManaged          = "shipohoy.managed"
	defaultLogBufferBytes = 128 * 1024
)

type logCapture struct {
	stdout   *ringBuffer
	stderr   *ringBuffer
	attached bool
}

func (l *logCapture) contains(stream shipohoy.LogStream, text []byte) bool {
	switch stream {
	case shipohoy.LogStdout:
		return bytesContains(l.stdout.Snapshot(), text)
	case shipohoy.LogStderr:
		return bytesContains(l.stderr.Snapshot(), text)
	case shipohoy.LogBoth:
		if bytesContains(l.stdout.Snapshot(), text) {
			return true
		}
		return bytesContains(l.stderr.Snapshot(), text)
	default:
		return false
	}
}

func bytesContains(buf []byte, text []byte) bool {
	if len(text) == 0 {
		return true
	}
	return bytes.Contains(buf, text)
}

func tailLines(data []byte, limit int) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

type ringBuffer struct {
	mu     sync.Mutex
	buf    []byte
	size   int
	start  int
	length int
}

func newRingBuffer(size int) *ringBuffer {
	if size < 0 {
		size = 0
	}
	return &ringBuffer{size: size}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	if r.size == 0 {
		return len(p), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf == nil {
		r.buf = make([]byte, r.size)
	}
	if len(p) >= r.size {
		copy(r.buf, p[len(p)-r.size:])
		r.start = 0
		r.length = r.size
		return len(p), nil
	}
	for _, b := range p {
		if r.length < r.size {
			idx := (r.start + r.length) % r.size
			r.buf[idx] = b
			r.length++
		} else {
			r.buf[r.start] = b
			r.start = (r.start + 1) % r.size
		}
	}
	return len(p), nil
}

func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.length == 0 {
		return nil
	}
	out := make([]byte, r.length)
	if r.start+r.length <= r.size {
		copy(out, r.buf[r.start:r.start+r.length])
		return out
	}
	n := r.size - r.start
	copy(out, r.buf[r.start:])
	copy(out[n:], r.buf[:r.length-n])
	return out
}

func (r *Runtime) logger(ctx context.Context) pslog.Logger {
	return pslog.Ctx(ctx).With("runtime", "containerd")
}
