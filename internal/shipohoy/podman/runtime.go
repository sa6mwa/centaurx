package podman

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/pslog"
)

const (
	labelManaged = "shipohoy.managed"
)

// Config configures the Podman runtime.
type Config struct {
	Address     string
	UserNSMode  string
	PullTimeout time.Duration
}

// Runtime implements shipohoy.Runtime using Podman's HTTP API.
type Runtime struct {
	client      *client
	pullTimeout time.Duration
	usernsMode  string
}

// New constructs a Podman runtime, trying fallback socket paths if needed.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	log := pslog.Ctx(ctx).With("runtime", "podman")
	addresses := candidateAddresses(cfg.Address)
	var lastErr error
	for _, addr := range addresses {
		log.Debug("podman connect attempt", "address", addr)
		cl, err := newClient(addr)
		if err != nil {
			log.Warn("podman connect failed", "address", addr, "err", err)
			lastErr = err
			continue
		}
		if err := cl.ping(ctx); err != nil {
			log.Warn("podman ping failed", "address", addr, "err", err)
			lastErr = err
			continue
		}
		timeout := cfg.PullTimeout
		if timeout == 0 {
			timeout = 5 * time.Minute
		}
		log.Info("podman runtime ready", "address", addr)
		return &Runtime{
			client:      cl,
			pullTimeout: timeout,
			usernsMode:  strings.TrimSpace(cfg.UserNSMode),
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("podman address not configured")
	}
	log.Warn("podman runtime unavailable", "err", lastErr)
	return nil, lastErr
}

// Close releases any resources held by the runtime.
func (r *Runtime) Close() error { return nil }

// ImageExists reports whether an image exists locally without pulling.
func (r *Runtime) ImageExists(ctx context.Context, image string) (bool, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		r.logger(ctx).Warn("podman image check rejected", "reason", "missing image")
		return false, errors.New("image is required")
	}
	log := r.logger(ctx).With("image", image)
	log.Debug("podman image exists check")
	res, err := r.client.do(ctx, "GET", fmt.Sprintf("/libpod/images/%s/exists", escapeImagePath(image)), nil, nil, "")
	if err != nil {
		log.Warn("podman image check failed", "err", err)
		return false, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 404 {
		log.Debug("podman image missing")
		return false, nil
	}
	if res.StatusCode >= 300 {
		log.Warn("podman image check failed", "status", res.StatusCode)
		return false, readAPIError(res)
	}
	log.Debug("podman image present")
	return true, nil
}

// EnsureImage pulls the image if it is not available.
func (r *Runtime) EnsureImage(ctx context.Context, image string) error {
	log := r.logger(ctx).With("image", image)
	log.Info("podman ensure image start")
	ok, err := r.ImageExists(ctx, image)
	if err != nil {
		log.Warn("podman ensure image failed", "err", err)
		return err
	}
	if ok {
		log.Info("podman ensure image ok")
		return nil
	}
	pullCtx, cancel := context.WithTimeout(ctx, r.pullTimeout)
	defer cancel()
	query := url.Values{}
	name, tag := splitImageRef(image)
	query.Set("fromImage", name)
	if tag != "" {
		query.Set("tag", tag)
	}
	res, err := r.client.do(pullCtx, "POST", "/images/create", query, nil, "")
	if err != nil {
		log.Warn("podman image pull failed", "err", err)
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		log.Warn("podman image pull failed", "status", res.StatusCode)
		return readAPIError(res)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	log.Info("podman ensure image ok")
	return nil
}

// EnsureRunning ensures a container exists and is running.
func (r *Runtime) EnsureRunning(ctx context.Context, spec shipohoy.ContainerSpec) (shipohoy.Handle, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return nil, errors.New("container name is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, errors.New("container image is required")
	}
	log := r.logger(ctx).With("container", spec.Name, "image", spec.Image)
	log.Info("podman ensure running start")
	inspect, exists, err := r.inspectContainer(ctx, spec.Name)
	if err != nil {
		log.Warn("podman inspect failed", "err", err)
		return nil, err
	}
	if !exists {
		created, err := r.createContainer(ctx, spec)
		if err != nil {
			log.Warn("podman create failed", "err", err)
			return nil, err
		}
		inspect.ID = created.ID
		inspect.Name = spec.Name
		inspect.State.Running = false
		log.Info("podman container created", "id", inspect.ID)
	}
	if !inspect.State.Running {
		if err := r.startContainer(ctx, inspect.ID); err != nil {
			log.Warn("podman start failed", "err", err)
			return nil, err
		}
		log.Info("podman container started", "id", inspect.ID)
	}
	log.Info("podman container ready", "id", inspect.ID)
	return &handle{name: spec.Name, id: inspect.ID}, nil
}

// Stop stops a running container.
func (r *Runtime) Stop(ctx context.Context, handle shipohoy.Handle) error {
	if handle == nil {
		return nil
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Info("podman stop start")
	query := url.Values{}
	query.Set("timeout", "10")
	res, err := r.client.do(ctx, "POST", fmt.Sprintf("/containers/%s/stop", url.PathEscape(handle.ID())), query, nil, "")
	if err != nil {
		log.Warn("podman stop failed", "err", err)
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 304 || res.StatusCode == 404 {
		log.Info("podman stop skipped", "status", res.StatusCode)
		return nil
	}
	if res.StatusCode >= 300 {
		log.Warn("podman stop failed", "status", res.StatusCode)
		return readAPIError(res)
	}
	log.Info("podman stop ok")
	return nil
}

// Remove removes a container.
func (r *Runtime) Remove(ctx context.Context, handle shipohoy.Handle) error {
	if handle == nil {
		return nil
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Info("podman remove start")
	query := url.Values{}
	query.Set("force", "true")
	res, err := r.client.do(ctx, "DELETE", fmt.Sprintf("/containers/%s", url.PathEscape(handle.ID())), query, nil, "")
	if err != nil {
		log.Warn("podman remove failed", "err", err)
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 404 {
		log.Info("podman remove skipped", "reason", "not found")
		return nil
	}
	if res.StatusCode >= 300 {
		log.Warn("podman remove failed", "status", res.StatusCode)
		return readAPIError(res)
	}
	log.Info("podman remove ok")
	return nil
}

// Exec runs a command in a running container.
func (r *Runtime) Exec(ctx context.Context, handle shipohoy.Handle, spec shipohoy.ExecSpec) (shipohoy.ExecResult, error) {
	if handle == nil {
		r.logger(ctx).Warn("podman exec rejected", "reason", "missing handle")
		return shipohoy.ExecResult{}, errors.New("container handle is required")
	}
	if len(spec.Command) == 0 {
		r.logger(ctx).Warn("podman exec rejected", "reason", "missing command")
		return shipohoy.ExecResult{}, errors.New("exec command is required")
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID(), "cmd_len", len(spec.Command))
	log.Info("podman exec start")
	if spec.Stdin != nil {
		log.Warn("podman exec failed", "err", "stdin not supported")
		return shipohoy.ExecResult{}, errors.New("podman exec stdin is not supported")
	}
	startTime := time.Now()
	ctx, cancel := withTimeout(ctx, spec.Timeout)
	defer cancel()

	execID, err := r.createExec(ctx, handle.ID(), spec)
	if err != nil {
		log.Warn("podman exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	if err := r.startExec(ctx, execID, spec.Stdout, spec.Stderr); err != nil {
		log.Warn("podman exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	code, err := r.inspectExec(ctx, execID)
	if err != nil {
		log.Warn("podman exec failed", "err", err)
		return shipohoy.ExecResult{}, err
	}
	finished := time.Now()
	if code != 0 {
		log.Warn("podman exec failed", "exit_code", code, "duration_ms", finished.Sub(startTime).Milliseconds())
	} else {
		log.Info("podman exec ok", "exit_code", code, "duration_ms", finished.Sub(startTime).Milliseconds())
	}
	return shipohoy.ExecResult{ExitCode: code, Started: startTime, Finished: finished}, nil
}

func (r *Runtime) logger(ctx context.Context) pslog.Logger {
	return pslog.Ctx(ctx).With("runtime", "podman")
}

// WaitForPort waits for a TCP port to accept connections.
func (r *Runtime) WaitForPort(ctx context.Context, _ shipohoy.Handle, spec shipohoy.WaitPortSpec) error {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	interval := spec.Interval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	address := strings.TrimSpace(spec.Address)
	if address == "" {
		address = "127.0.0.1"
	}
	target := net.JoinHostPort(address, fmt.Sprintf("%d", spec.Port))
	log := r.logger(ctx).With("target", target)
	log.Debug("podman wait for port start", "timeout_ms", timeout.Milliseconds())
	deadline := time.Now().Add(timeout)
	dialer := &net.Dialer{Timeout: 1 * time.Second}
	for {
		conn, err := dialer.DialContext(ctx, "tcp", target)
		if err == nil {
			_ = conn.Close()
			log.Debug("podman wait for port ok")
			return nil
		}
		if time.Now().After(deadline) {
			log.Warn("podman wait for port failed", "err", err)
			return fmt.Errorf("port %s not ready: %w", target, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// WaitForLog waits for a log substring.
func (r *Runtime) WaitForLog(ctx context.Context, handle shipohoy.Handle, spec shipohoy.WaitLogSpec) error {
	if handle == nil {
		r.logger(ctx).Warn("podman wait for log rejected", "reason", "missing handle")
		return errors.New("container handle is required")
	}
	text := strings.TrimSpace(spec.Text)
	if text == "" {
		r.logger(ctx).Warn("podman wait for log rejected", "reason", "missing text")
		return errors.New("log text is required")
	}
	log := r.logger(ctx).With("container", handle.Name(), "id", handle.ID())
	log.Debug("podman wait for log start")
	ctx, cancel := withTimeout(ctx, spec.Timeout)
	defer cancel()
	query := url.Values{}
	query.Set("follow", "1")
	query.Set("since", "0")
	switch spec.Stream {
	case shipohoy.LogStdout:
		query.Set("stdout", "1")
	case shipohoy.LogStderr:
		query.Set("stderr", "1")
	default:
		query.Set("stdout", "1")
		query.Set("stderr", "1")
	}
	res, err := r.client.do(ctx, "GET", fmt.Sprintf("/containers/%s/logs", url.PathEscape(handle.ID())), query, nil, "")
	if err != nil {
		log.Warn("podman wait for log failed", "err", err)
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		log.Warn("podman wait for log failed", "status", res.StatusCode)
		return readAPIError(res)
	}
	if err := searchDockerStream(ctx, res.Body, text); err != nil {
		log.Warn("podman wait for log failed", "err", err)
		return fmt.Errorf("log text %q not found: %w", text, err)
	}
	log.Debug("podman wait for log ok")
	return nil
}

// TailLogs returns the last N log lines for a container.
func (r *Runtime) TailLogs(ctx context.Context, handle shipohoy.Handle, limit int) ([]string, []string, error) {
	if handle == nil {
		return nil, nil, errors.New("container handle is required")
	}
	if limit <= 0 {
		limit = 50
	}
	query := url.Values{}
	query.Set("follow", "0")
	query.Set("since", "0")
	query.Set("tail", strconv.Itoa(limit))
	query.Set("stdout", "1")
	query.Set("stderr", "1")
	res, err := r.client.do(ctx, "GET", fmt.Sprintf("/containers/%s/logs", url.PathEscape(handle.ID())), query, nil, "")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, nil, readAPIError(res)
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, err
	}
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if err := copyDockerStream(bytes.NewReader(data), &stdoutBuf, &stderrBuf); err != nil {
		stdoutBuf.Reset()
		_, _ = stdoutBuf.Write(data)
	}
	stdout := tailLines(stdoutBuf.String(), limit)
	stderr := tailLines(stderrBuf.String(), limit)
	return stdout, stderr, nil
}

// Janitor prunes managed containers by label.
func (r *Runtime) Janitor(ctx context.Context, spec shipohoy.JanitorSpec) (int, error) {
	log := r.logger(ctx)
	log.Info("podman janitor start")
	filters := map[string][]string{}
	labels := []string{labelManaged + "=true"}
	for k, v := range spec.LabelSelector {
		if strings.TrimSpace(k) == "" {
			continue
		}
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}
	filters["label"] = labels
	filterJSON, err := json.Marshal(filters)
	if err != nil {
		log.Warn("podman janitor failed", "err", err)
		return 0, err
	}
	query := url.Values{}
	query.Set("all", "1")
	query.Set("filters", string(filterJSON))
	res, err := r.client.do(ctx, "GET", "/containers/json", query, nil, "")
	if err != nil {
		log.Warn("podman janitor failed", "err", err)
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		log.Warn("podman janitor failed", "status", res.StatusCode)
		return 0, readAPIError(res)
	}
	var list []containerListItem
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		log.Warn("podman janitor failed", "err", err)
		return 0, err
	}
	removed := 0
	cutoff := time.Now().Add(-spec.MinAge)
	for _, item := range list {
		if spec.MinAge > 0 {
			created := time.Unix(item.Created, 0)
			if created.After(cutoff) {
				continue
			}
		}
		autoRemove := false
		if inspect, ok, err := r.inspectContainer(ctx, item.ID); err == nil && ok {
			autoRemove = inspect.HostConfig.AutoRemove
		}
		h := &handle{name: containerName(item), id: item.ID}
		_ = r.Stop(ctx, h)
		if autoRemove {
			removed++
			continue
		}
		if err := r.Remove(ctx, h); err != nil {
			log.Warn("podman janitor failed", "err", err)
			return removed, err
		}
		removed++
	}
	log.Info("podman janitor ok", "removed", removed)
	return removed, nil
}

func (r *Runtime) inspectContainer(ctx context.Context, name string) (inspectContainer, bool, error) {
	res, err := r.client.do(ctx, "GET", fmt.Sprintf("/containers/%s/json", url.PathEscape(name)), nil, nil, "")
	if err != nil {
		return inspectContainer{}, false, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 404 {
		return inspectContainer{}, false, nil
	}
	if res.StatusCode >= 300 {
		return inspectContainer{}, false, readAPIError(res)
	}
	var inspect inspectContainer
	if err := json.NewDecoder(res.Body).Decode(&inspect); err != nil {
		return inspectContainer{}, false, err
	}
	return inspect, true, nil
}

func (r *Runtime) createContainer(ctx context.Context, spec shipohoy.ContainerSpec) (createResponse, error) {
	labels := mergeLabels(spec.Labels, map[string]string{labelManaged: "true"})
	req := map[string]any{
		"Image":      spec.Image,
		"Cmd":        spec.Command,
		"WorkingDir": spec.WorkingDir,
		"Labels":     labels,
	}
	env := envMapToSlice(spec.Env)
	if len(env) > 0 {
		req["Env"] = env
	}
	hostConfig := map[string]any{}
	if spec.HostNetwork {
		hostConfig["NetworkMode"] = "host"
	}
	if spec.ReadOnlyRootfs {
		hostConfig["ReadonlyRootfs"] = true
	}
	if spec.AutoRemove {
		hostConfig["AutoRemove"] = true
	}
	if r.usernsMode != "" {
		hostConfig["UsernsMode"] = r.usernsMode
	}
	if spec.ResourceCaps != nil {
		if spec.ResourceCaps.MemoryBytes > 0 {
			hostConfig["Memory"] = spec.ResourceCaps.MemoryBytes
		}
		if spec.ResourceCaps.NanoCPUs > 0 {
			hostConfig["NanoCPUs"] = spec.ResourceCaps.NanoCPUs
		}
	}
	if binds := buildBinds(spec.Mounts); len(binds) > 0 {
		hostConfig["Binds"] = binds
	}
	if tmpfs := buildTmpfs(spec.Tmpfs); len(tmpfs) > 0 {
		hostConfig["Tmpfs"] = tmpfs
	}
	if len(hostConfig) > 0 {
		req["HostConfig"] = hostConfig
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return createResponse{}, err
	}
	query := url.Values{}
	query.Set("name", spec.Name)
	res, err := r.client.do(ctx, "POST", "/containers/create", query, bytes.NewReader(payload), "application/json")
	if err != nil {
		return createResponse{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return createResponse{}, readAPIError(res)
	}
	var created createResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		return createResponse{}, err
	}
	if created.ID == "" {
		return createResponse{}, errors.New("podman create did not return container id")
	}
	return created, nil
}

func (r *Runtime) startContainer(ctx context.Context, id string) error {
	res, err := r.client.do(ctx, "POST", fmt.Sprintf("/containers/%s/start", url.PathEscape(id)), nil, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 304 {
		return nil
	}
	if res.StatusCode >= 300 {
		return readAPIError(res)
	}
	return nil
}

func (r *Runtime) createExec(ctx context.Context, id string, spec shipohoy.ExecSpec) (string, error) {
	req := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          spec.Command,
		"Tty":          false,
	}
	if spec.WorkingDir != "" {
		req["WorkingDir"] = spec.WorkingDir
	}
	if env := envMapToSlice(spec.Env); len(env) > 0 {
		req["Env"] = env
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	res, err := r.client.do(ctx, "POST", fmt.Sprintf("/containers/%s/exec", url.PathEscape(id)), nil, bytes.NewReader(payload), "application/json")
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return "", readAPIError(res)
	}
	var resp execCreateResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", errors.New("podman exec did not return id")
	}
	return resp.ID, nil
}

func (r *Runtime) startExec(ctx context.Context, id string, stdout, stderr io.Writer) error {
	payload, err := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return err
	}
	res, err := r.client.do(ctx, "POST", fmt.Sprintf("/exec/%s/start", url.PathEscape(id)), nil, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return readAPIError(res)
	}
	return copyDockerStream(res.Body, stdout, stderr)
}

func (r *Runtime) inspectExec(ctx context.Context, id string) (int, error) {
	res, err := r.client.do(ctx, "GET", fmt.Sprintf("/exec/%s/json", url.PathEscape(id)), nil, nil, "")
	if err != nil {
		return -1, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return -1, readAPIError(res)
	}
	var inspect execInspect
	if err := json.NewDecoder(res.Body).Decode(&inspect); err != nil {
		return -1, err
	}
	if inspect.Running {
		return -1, errors.New("exec still running")
	}
	return inspect.ExitCode, nil
}

func mergeLabels(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		out[k] = v
	}
	return out
}

func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

func buildBinds(mounts []shipohoy.Mount) []string {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(mounts))
	for _, m := range mounts {
		if strings.TrimSpace(m.Source) == "" || strings.TrimSpace(m.Target) == "" {
			continue
		}
		options := []string{}
		if m.ReadOnly {
			options = append(options, "ro")
		}
		if m.Propagation != "" {
			options = append(options, m.Propagation)
		}
		entry := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if len(options) > 0 {
			entry = entry + ":" + strings.Join(options, ",")
		}
		out = append(out, entry)
	}
	return out
}

func buildTmpfs(tmpfs []shipohoy.TmpfsMount) map[string]string {
	if len(tmpfs) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, m := range tmpfs {
		if strings.TrimSpace(m.Target) == "" {
			continue
		}
		out[m.Target] = strings.Join(m.Options, ",")
	}
	return out
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func copyDockerStream(r io.Reader, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		var dst io.Writer
		switch header[0] {
		case 1:
			dst = stdout
		case 2:
			dst = stderr
		default:
			dst = stdout
		}
		if _, err := io.CopyN(dst, r, int64(size)); err != nil {
			return err
		}
	}
}

func searchDockerStream(ctx context.Context, r io.Reader, needle string) error {
	if needle == "" {
		return nil
	}
	search := newStreamSearcher([]byte(needle))
	reader := bufio.NewReader(r)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		header := make([]byte, 8)
		if _, err := io.ReadFull(reader, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return errors.New("log stream ended")
			}
			return err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		if err := search.consume(reader, int64(size)); err != nil {
			if errors.Is(err, errFound) {
				return nil
			}
			return err
		}
	}
}

func tailLines(text string, limit int) []string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

var errFound = errors.New("found")

type streamSearcher struct {
	needle []byte
	tail   []byte
}

func newStreamSearcher(needle []byte) *streamSearcher {
	return &streamSearcher{needle: needle}
}

func (s *streamSearcher) consume(r io.Reader, size int64) error {
	buf := make([]byte, 32*1024)
	remaining := size
	for remaining > 0 {
		chunk := int64(len(buf))
		if remaining < chunk {
			chunk = remaining
		}
		n, err := io.ReadFull(r, buf[:chunk])
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return err
			}
			return err
		}
		remaining -= int64(n)
		window := append(s.tail, buf[:n]...)
		if bytes.Contains(window, s.needle) {
			return errFound
		}
		if len(s.needle) > 1 {
			if len(window) >= len(s.needle)-1 {
				s.tail = append([]byte(nil), window[len(window)-(len(s.needle)-1):]...)
			} else {
				s.tail = append([]byte(nil), window...)
			}
		}
	}
	return nil
}

func containerName(item containerListItem) string {
	if len(item.Names) == 0 {
		return ""
	}
	name := item.Names[0]
	return strings.TrimPrefix(name, "/")
}

func splitImageRef(image string) (string, string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", ""
	}
	if at := strings.Index(image, "@"); at != -1 {
		return image, ""
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon], image[lastColon+1:]
	}
	return image, ""
}

// handle represents a podman container handle.
type handle struct {
	name string
	id   string
}

func (h *handle) Name() string { return h.name }
func (h *handle) ID() string   { return h.id }
