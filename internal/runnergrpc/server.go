package runnergrpc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/runnerpb"
	"pkt.systems/centaurx/internal/usage"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// Server implements the runner gRPC service and provides a ListenAndServe entrypoint.
type Server struct {
	runnerpb.UnimplementedRunnerServer

	cfg    Config
	runner core.Runner
	logger pslog.Logger

	mu   sync.Mutex
	runs map[string]runProcess

	lastPingUnix int64
}

type runProcess interface {
	Signal(sig core.ProcessSignal) error
}

type handleProcess struct {
	handle core.RunHandle
}

func (p handleProcess) Signal(sig core.ProcessSignal) error {
	return p.handle.Signal(context.Background(), sig)
}

type cmdProcess struct {
	cmd  *exec.Cmd
	pgid int
}

func (p cmdProcess) Signal(sig core.ProcessSignal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return errors.New("process not started")
	}
	pid := p.cmd.Process.Pid
	if pid <= 0 {
		return errors.New("invalid process id")
	}
	signal := syscall.Signal(0)
	switch sig {
	case core.ProcessSignalHUP:
		signal = syscall.SIGHUP
	case core.ProcessSignalTERM:
		signal = syscall.SIGTERM
	case core.ProcessSignalKILL:
		signal = syscall.SIGKILL
	default:
		return fmt.Errorf("unsupported signal: %s", sig)
	}
	if signal == 0 {
		return fmt.Errorf("unsupported signal: %s", sig)
	}
	if p.pgid > 0 {
		if err := syscall.Kill(-p.pgid, signal); err == nil {
			killProcessTree(pid, signal)
			return nil
		}
	}
	if err := syscall.Kill(-pid, signal); err == nil {
		killProcessTree(pid, signal)
		return nil
	}
	if err := p.cmd.Process.Signal(signal); err != nil {
		return err
	}
	killProcessTree(pid, signal)
	return nil
}

// NewServer constructs a runner gRPC server.
func NewServer(cfg Config, runner core.Runner) *Server {
	return &Server{cfg: cfg, runner: runner, runs: make(map[string]runProcess)}
}

// ListenAndServe starts the gRPC server over a Unix domain socket.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.cfg.SocketPath == "" {
		return errors.New("runner socket path is required")
	}
	if s.logger == nil {
		s.logger = pslog.Ctx(ctx)
	}
	if s.cfg.KeepaliveMisses <= 0 {
		s.cfg.KeepaliveMisses = 3
	}
	if s.cfg.KeepaliveInterval <= 0 {
		s.cfg.KeepaliveInterval = 10 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.SocketPath)

	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	runnerpb.RegisterRunnerServer(grpcServer, s)
	s.logger.Info("runner grpc listening", "socket", s.cfg.SocketPath)

	errCh := make(chan error, 1)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.setLastPing(time.Now())
	if s.cfg.KeepaliveInterval > 0 && s.cfg.KeepaliveMisses > 0 {
		go s.keepaliveLoop(runCtx, cancel, grpcServer)
	}
	go func() {
		errCh <- grpcServer.Serve(listener)
	}()

	select {
	case <-runCtx.Done():
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

// Ping updates the keepalive timer.
func (s *Server) Ping(ctx context.Context, _ *runnerpb.PingRequest) (*runnerpb.PingResponse, error) {
	s.setLastPing(time.Now())
	s.log(ctx).Trace("runner ping")
	return &runnerpb.PingResponse{Ok: true}, nil
}

// GetUsage fetches account usage for ChatGPT logins.
func (s *Server) GetUsage(ctx context.Context, _ *runnerpb.UsageRequest) (*runnerpb.UsageResponse, error) {
	info, err := usage.Fetch(ctx)
	if err != nil {
		s.log(ctx).Warn("runner usage fetch failed", "err", err)
		return nil, status.Errorf(codes.Internal, "usage fetch failed: %v", err)
	}
	return &runnerpb.UsageResponse{
		Chatgpt:         info.ChatGPT,
		PrimaryWindow:   toPBUsageWindow(info.Primary),
		SecondaryWindow: toPBUsageWindow(info.Secondary),
	}, nil
}

// Exec runs a new codex exec session.
func (s *Server) Exec(req *runnerpb.ExecRequest, stream runnerpb.Runner_ExecServer) error {
	if err := validateExec(req); err != nil {
		s.log(stream.Context()).Warn("runner exec rejected", "run_id", req.GetRunId(), "err", err)
		return err
	}
	log := s.log(stream.Context()).With("run_id", req.RunId)
	started := time.Now()
	log.Info("runner exec start", "model", req.Model, "reasoning_effort", req.ModelReasoningEffort, "json", req.Json)
	log.Debug("runner exec request", "workdir", req.WorkingDir, "ssh_auth_sock", req.SshAuthSock != "", "prompt_len", len(req.Prompt))
	if len(req.Prompt) > 0 {
		log.Trace("runner exec prompt", "preview", previewText(req.Prompt, 200), "truncated", len(req.Prompt) > 200)
	}
	runCtx := pslog.ContextWithLogger(stream.Context(), log)
	handle, err := s.runner.Run(runCtx, core.RunRequest{
		WorkingDir:           req.WorkingDir,
		Prompt:               req.Prompt,
		Model:                schema.ModelID(req.Model),
		ModelReasoningEffort: schema.ModelReasoningEffort(req.ModelReasoningEffort),
		JSON:                 req.Json,
		SSHAuthSock:          req.SshAuthSock,
	})
	if err != nil {
		log.Error("runner exec failed", "err", err)
		return status.Errorf(codes.Internal, "exec failed: %v", err)
	}
	s.register(req.RunId, handleProcess{handle: handle})
	defer s.unregister(req.RunId)
	defer func() { _ = handle.Close() }()

	if err := stream.Send(&runnerpb.RunnerEvent{
		RunId: req.RunId,
		Payload: &runnerpb.RunnerEvent_Status{
			Status: &runnerpb.RunStatus{State: runnerpb.RunState_RUN_STATE_STARTED},
		},
	}); err != nil {
		log.Warn("runner exec stream start failed", "err", err)
		return err
	}

	count, err := s.streamExecEvents(stream, req.RunId, handle.Events())
	if err != nil {
		log.Warn("runner exec stream failed", "err", err, "events", count)
		return err
	}
	return s.finishRun(stream, req.RunId, handle, started, count)
}

// ExecResume resumes an existing codex exec session.
func (s *Server) ExecResume(req *runnerpb.ExecResumeRequest, stream runnerpb.Runner_ExecResumeServer) error {
	if err := validateExecResume(req); err != nil {
		s.log(stream.Context()).Warn("runner exec resume rejected", "run_id", req.GetRunId(), "err", err)
		return err
	}
	log := s.log(stream.Context()).With("run_id", req.RunId)
	started := time.Now()
	log.Info("runner exec resume start", "model", req.Model, "reasoning_effort", req.ModelReasoningEffort, "json", req.Json, "resume_id", req.ResumeSessionId)
	log.Debug("runner exec resume request", "workdir", req.WorkingDir, "ssh_auth_sock", req.SshAuthSock != "", "prompt_len", len(req.Prompt))
	if len(req.Prompt) > 0 {
		log.Trace("runner exec resume prompt", "preview", previewText(req.Prompt, 200), "truncated", len(req.Prompt) > 200)
	}
	runCtx := pslog.ContextWithLogger(stream.Context(), log)
	handle, err := s.runner.Run(runCtx, core.RunRequest{
		WorkingDir:           req.WorkingDir,
		Prompt:               req.Prompt,
		Model:                schema.ModelID(req.Model),
		ModelReasoningEffort: schema.ModelReasoningEffort(req.ModelReasoningEffort),
		ResumeSessionID:      schema.SessionID(req.ResumeSessionId),
		JSON:                 req.Json,
		SSHAuthSock:          req.SshAuthSock,
	})
	if err != nil {
		log.Error("runner exec resume failed", "err", err)
		return status.Errorf(codes.Internal, "exec resume failed: %v", err)
	}
	s.register(req.RunId, handleProcess{handle: handle})
	defer s.unregister(req.RunId)
	defer func() { _ = handle.Close() }()

	if err := stream.Send(&runnerpb.RunnerEvent{
		RunId: req.RunId,
		Payload: &runnerpb.RunnerEvent_Status{
			Status: &runnerpb.RunStatus{State: runnerpb.RunState_RUN_STATE_STARTED},
		},
	}); err != nil {
		log.Warn("runner exec resume stream start failed", "err", err)
		return err
	}

	count, err := s.streamExecEvents(stream, req.RunId, handle.Events())
	if err != nil {
		log.Warn("runner exec resume stream failed", "err", err, "events", count)
		return err
	}
	return s.finishRun(stream, req.RunId, handle, started, count)
}

// RunCommand executes a shell command in the working directory.
func (s *Server) RunCommand(req *runnerpb.RunCommandRequest, stream runnerpb.Runner_RunCommandServer) error {
	if strings.TrimSpace(req.RunId) == "" {
		s.log(stream.Context()).Warn("runner command rejected", "err", "run_id required")
		return status.Error(codes.InvalidArgument, "run_id is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		s.log(stream.Context()).Warn("runner command rejected", "run_id", req.RunId, "err", "command required")
		return status.Error(codes.InvalidArgument, "command is required")
	}
	log := s.log(stream.Context()).With("run_id", req.RunId)
	started := time.Now()
	log.Info("runner command start", "workdir", req.WorkingDir, "shell", req.UseShell)
	log.Debug("runner command request", "command_len", len(req.Command), "ssh_auth_sock", req.SshAuthSock != "")
	log.Trace("runner command", "command", req.Command)

	cmd, err := commandForRequest(req)
	if err != nil {
		log.Warn("runner command rejected", "err", err)
		return status.Errorf(codes.InvalidArgument, "command invalid: %v", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	if req.SshAuthSock != "" {
		cmd.Env = append(filterEnv(os.Environ(), "SSH_AUTH_SOCK"), fmt.Sprintf("SSH_AUTH_SOCK=%s", req.SshAuthSock))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("runner command stdout failed", "err", err)
		return status.Errorf(codes.Internal, "stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("runner command stderr failed", "err", err)
		return status.Errorf(codes.Internal, "stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Error("runner command start failed", "err", err)
		return status.Errorf(codes.Internal, "command start: %v", err)
	}
	applyNice(log, cmd.Process.Pid, s.cfg.CommandNice, "command")

	pgid, _ := syscall.Getpgid(cmd.Process.Pid)
	s.register(req.RunId, cmdProcess{cmd: cmd, pgid: pgid})
	defer s.unregister(req.RunId)

	if err := stream.Send(&runnerpb.RunnerEvent{
		RunId: req.RunId,
		Payload: &runnerpb.RunnerEvent_Status{
			Status: &runnerpb.RunStatus{State: runnerpb.RunState_RUN_STATE_STARTED},
		},
	}); err != nil {
		log.Warn("runner command stream start failed", "err", err)
		return err
	}

	outputCh := make(chan *runnerpb.CommandOutput, 128)
	var wg sync.WaitGroup
	wg.Add(2)
	go readCommandStream(&wg, stdout, runnerpb.StreamKind_STREAM_KIND_STDOUT, outputCh)
	go readCommandStream(&wg, stderr, runnerpb.StreamKind_STREAM_KIND_STDERR, outputCh)
	go func() {
		wg.Wait()
		close(outputCh)
	}()

	stdoutLines := 0
	stderrLines := 0
	for output := range outputCh {
		switch output.Stream {
		case runnerpb.StreamKind_STREAM_KIND_STDOUT:
			stdoutLines++
		case runnerpb.StreamKind_STREAM_KIND_STDERR:
			stderrLines++
		}
		event := &runnerpb.RunnerEvent{
			RunId: req.RunId,
			Payload: &runnerpb.RunnerEvent_CommandOutput{
				CommandOutput: output,
			},
		}
		if err := stream.Send(event); err != nil {
			log.Warn("runner command stream failed", "err", err)
			_ = cmd.Wait()
			return err
		}
	}

	err = cmd.Wait()
	exitCode := 0
	state := runnerpb.RunState_RUN_STATE_FINISHED
	if err != nil {
		state = runnerpb.RunState_RUN_STATE_FAILED
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	if err != nil {
		log.Warn(
			"runner command finished",
			"state", state.String(),
			"exit_code", exitCode,
			"stdout_lines", stdoutLines,
			"stderr_lines", stderrLines,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", err,
		)
	} else {
		log.Info(
			"runner command finished",
			"state", state.String(),
			"exit_code", exitCode,
			"stdout_lines", stdoutLines,
			"stderr_lines", stderrLines,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
	return stream.Send(&runnerpb.RunnerEvent{
		RunId: req.RunId,
		Payload: &runnerpb.RunnerEvent_Status{
			Status: &runnerpb.RunStatus{State: state, ExitCode: int32(exitCode)},
		},
	})
}

// SignalSession sends a signal to a running session.
func (s *Server) SignalSession(ctx context.Context, req *runnerpb.SignalRequest) (*runnerpb.SignalResponse, error) {
	if strings.TrimSpace(req.RunId) == "" {
		s.log(ctx).Warn("runner signal rejected", "err", "run_id required")
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	log := s.log(ctx).With("run_id", req.RunId)
	proc := s.lookup(req.RunId)
	if proc == nil {
		log.Info("runner signal ignored", "reason", "not running")
		return &runnerpb.SignalResponse{Ok: false, Message: "not running"}, nil
	}
	signal := fromPBSignal(req.Signal)
	if signal == "" {
		log.Warn("runner signal ignored", "reason", "invalid signal")
		return &runnerpb.SignalResponse{Ok: false, Message: "invalid signal"}, nil
	}
	if err := proc.Signal(signal); err != nil {
		log.Warn("runner signal failed", "signal", signal, "err", err)
		return &runnerpb.SignalResponse{Ok: false, Message: err.Error()}, nil
	}
	log.Info("runner signal sent", "signal", signal)
	return &runnerpb.SignalResponse{Ok: true}, nil
}

func (s *Server) register(runID string, proc runProcess) {
	s.mu.Lock()
	s.runs[runID] = proc
	s.mu.Unlock()
}

func (s *Server) unregister(runID string) {
	s.mu.Lock()
	delete(s.runs, runID)
	s.mu.Unlock()
}

func (s *Server) lookup(runID string) runProcess {
	s.mu.Lock()
	proc := s.runs[runID]
	s.mu.Unlock()
	return proc
}

type execStream interface {
	Context() context.Context
	Send(*runnerpb.RunnerEvent) error
}

func (s *Server) streamExecEvents(stream execStream, runID string, events core.EventStream) (int, error) {
	count := 0
	log := s.log(stream.Context()).With("run_id", runID)
	for {
		event, err := events.Next(stream.Context())
		if err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			log.Warn("runner exec stream read failed", "err", err, "events", count)
			return count, status.Errorf(codes.Internal, "stream error: %v", err)
		}
		count++
		payload := &runnerpb.RunnerEvent{
			RunId: runID,
			Payload: &runnerpb.RunnerEvent_Exec{
				Exec: toPBExecEvent(event),
			},
		}
		if err := stream.Send(payload); err != nil {
			log.Warn("runner exec stream send failed", "err", err, "events", count)
			return count, err
		}
	}
}

func (s *Server) finishRun(stream execStream, runID string, handle core.RunHandle, started time.Time, eventCount int) error {
	result, err := handle.Wait(stream.Context())
	log := s.log(stream.Context()).With("run_id", runID)
	state := runnerpb.RunState_RUN_STATE_FINISHED
	message := ""
	if err != nil {
		state = runnerpb.RunState_RUN_STATE_FAILED
		message = err.Error()
	}
	fields := []any{
		"state", state.String(),
		"exit_code", result.ExitCode,
		"events", eventCount,
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if err != nil {
		fields = append(fields, "err", err)
	}
	if err != nil {
		log.Warn("runner exec finished", fields...)
	} else {
		log.Info("runner exec finished", fields...)
	}
	if err := stream.Send(&runnerpb.RunnerEvent{
		RunId: runID,
		Payload: &runnerpb.RunnerEvent_Status{
			Status: &runnerpb.RunStatus{State: state, ExitCode: int32(result.ExitCode), Message: message},
		},
	}); err != nil {
		log.Warn("runner exec status send failed", "err", err)
		return err
	}
	return nil
}

func validateExec(req *runnerpb.ExecRequest) error {
	if strings.TrimSpace(req.RunId) == "" {
		return status.Error(codes.InvalidArgument, "run_id is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return status.Error(codes.InvalidArgument, "prompt is required")
	}
	return nil
}

func validateExecResume(req *runnerpb.ExecResumeRequest) error {
	if strings.TrimSpace(req.RunId) == "" {
		return status.Error(codes.InvalidArgument, "run_id is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return status.Error(codes.InvalidArgument, "prompt is required")
	}
	if strings.TrimSpace(req.ResumeSessionId) == "" {
		return status.Error(codes.InvalidArgument, "resume_session_id is required")
	}
	return nil
}

func commandForRequest(req *runnerpb.RunCommandRequest) (*exec.Cmd, error) {
	if req.UseShell {
		return exec.Command("sh", "-lc", req.Command), nil
	}
	parts := strings.Fields(req.Command)
	if len(parts) == 0 {
		return nil, errors.New("command is empty")
	}
	return exec.Command(parts[0], parts[1:]...), nil
}

func readCommandStream(wg *sync.WaitGroup, reader io.Reader, kind runnerpb.StreamKind, outputCh chan<- *runnerpb.CommandOutput) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		outputCh <- &runnerpb.CommandOutput{Stream: kind, Text: text}
	}
}

func fromPBSignal(sig runnerpb.ProcessSignal) core.ProcessSignal {
	switch sig {
	case runnerpb.ProcessSignal_PROCESS_SIGNAL_HUP:
		return core.ProcessSignalHUP
	case runnerpb.ProcessSignal_PROCESS_SIGNAL_TERM:
		return core.ProcessSignalTERM
	case runnerpb.ProcessSignal_PROCESS_SIGNAL_KILL:
		return core.ProcessSignalKILL
	default:
		return ""
	}
}

func filterEnv(env []string, key string) []string {
	if len(env) == 0 {
		return env
	}
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) log(ctx context.Context) pslog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return pslog.Ctx(ctx)
}

func (s *Server) setLastPing(ts time.Time) {
	atomic.StoreInt64(&s.lastPingUnix, ts.UnixNano())
}

func (s *Server) lastPing() time.Time {
	val := atomic.LoadInt64(&s.lastPingUnix)
	if val == 0 {
		return time.Time{}
	}
	return time.Unix(0, val)
}

func (s *Server) keepaliveLoop(ctx context.Context, cancel context.CancelFunc, grpcServer *grpc.Server) {
	ticker := time.NewTicker(s.cfg.KeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := s.lastPing()
			if last.IsZero() {
				continue
			}
			if time.Since(last) > time.Duration(s.cfg.KeepaliveMisses)*s.cfg.KeepaliveInterval {
				s.logger.Warn("runner keepalive missed; shutting down", "last_ping", last.Format(time.RFC3339Nano), "interval", s.cfg.KeepaliveInterval, "misses", s.cfg.KeepaliveMisses)
				grpcServer.GracefulStop()
				cancel()
				return
			}
		}
	}
}

func killProcessTree(root int, sig syscall.Signal) {
	if root <= 0 {
		return
	}
	children, err := listProcessChildren(root)
	if err != nil {
		return
	}
	for _, pid := range children {
		_ = syscall.Kill(pid, sig)
	}
}

func listProcessChildren(root int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parents := make(map[int][]int)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		ppid, err := readPPid(pid)
		if err != nil {
			continue
		}
		parents[ppid] = append(parents[ppid], pid)
	}
	var out []int
	queue := []int{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range parents[cur] {
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out, nil
}

func readPPid(pid int) (int, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "status")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, errors.New("ppid missing")
			}
			ppid, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return ppid, nil
		}
	}
	return 0, errors.New("ppid not found")
}

func applyNice(log pslog.Logger, pid int, nice int, label string) {
	if nice == 0 || pid <= 0 {
		return
	}
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, nice); err != nil {
		if log != nil {
			log.Warn("runner nice set failed", "process", label, "pid", pid, "nice", nice, "err", err)
		}
		return
	}
	if log != nil {
		log.Debug("runner nice set", "process", label, "pid", pid, "nice", nice)
	}
}

func previewText(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}
