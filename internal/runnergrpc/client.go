package runnergrpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/runnerpb"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// Client implements core.Runner over gRPC.
type Client struct {
	conn   *grpc.ClientConn
	client runnerpb.RunnerClient
}

// Dial creates a new runner client over a Unix domain socket.
func Dial(ctx context.Context, socketPath string) (*Client, error) {
	if socketPath == "" {
		return nil, errors.New("runner socket path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return net.Dial("unix", addr)
	}
	target := "passthrough:///" + socketPath
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, client: runnerpb.NewRunnerClient(conn)}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Ping sends a keepalive ping to the runner.
func (c *Client) Ping(ctx context.Context) error {
	if c.client == nil {
		return errors.New("runner client not initialized")
	}
	_, err := c.client.Ping(ctx, &runnerpb.PingRequest{})
	return err
}

// Usage fetches account usage information.
func (c *Client) Usage(ctx context.Context) (core.UsageInfo, error) {
	if c.client == nil {
		return core.UsageInfo{}, errors.New("runner client not initialized")
	}
	resp, err := c.client.GetUsage(ctx, &runnerpb.UsageRequest{})
	if err != nil {
		return core.UsageInfo{}, err
	}
	return core.UsageInfo{
		ChatGPT:   resp.GetChatgpt(),
		Primary:   fromPBUsageWindow(resp.GetPrimaryWindow()),
		Secondary: fromPBUsageWindow(resp.GetSecondaryWindow()),
	}, nil
}

// Run starts a codex exec session via gRPC.
func (c *Client) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	runID := newRunID()
	log := pslog.Ctx(ctx).With("run_id", runID)
	log.Info("runner grpc exec start", "model", req.Model, "reasoning_effort", req.ModelReasoningEffort, "json", req.JSON)
	log.Debug("runner grpc exec request", "workdir", req.WorkingDir, "ssh_auth_sock", req.SSHAuthSock != "", "prompt_len", len(req.Prompt), "resume", req.ResumeSessionID != "")
	if req.ResumeSessionID != "" {
		stream, err := c.client.ExecResume(ctx, &runnerpb.ExecResumeRequest{
			RunId:                runID,
			WorkingDir:           req.WorkingDir,
			Prompt:               req.Prompt,
			Model:                string(req.Model),
			ModelReasoningEffort: string(req.ModelReasoningEffort),
			ResumeSessionId:      string(req.ResumeSessionID),
			Json:                 req.JSON,
			SshAuthSock:          req.SSHAuthSock,
		})
		if err != nil {
			logGRPCError(log, "runner grpc exec failed", err)
			return nil, wrapRunnerError("exec", err)
		}
		return newRunHandle(c.client, runID, stream, log), nil
	}
	stream, err := c.client.Exec(ctx, &runnerpb.ExecRequest{
		RunId:                runID,
		WorkingDir:           req.WorkingDir,
		Prompt:               req.Prompt,
		Model:                string(req.Model),
		ModelReasoningEffort: string(req.ModelReasoningEffort),
		Json:                 req.JSON,
		SshAuthSock:          req.SSHAuthSock,
	})
	if err != nil {
		logGRPCError(log, "runner grpc exec failed", err)
		return nil, wrapRunnerError("exec", err)
	}
	return newRunHandle(c.client, runID, stream, log), nil
}

// RunCommand executes a command via gRPC.
func (c *Client) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	runID := newRunID()
	log := pslog.Ctx(ctx).With("run_id", runID)
	log.Trace("runner grpc command start", "shell", req.UseShell)
	log.Debug("runner grpc command request", "workdir", req.WorkingDir, "command_len", len(req.Command), "ssh_auth_sock", req.SSHAuthSock != "")
	if req.Command != "" {
		log.Trace("runner grpc command", "command", req.Command)
	}
	stream, err := c.client.RunCommand(ctx, &runnerpb.RunCommandRequest{
		RunId:       runID,
		WorkingDir:  req.WorkingDir,
		Command:     req.Command,
		UseShell:    req.UseShell,
		SshAuthSock: req.SSHAuthSock,
	})
	if err != nil {
		logGRPCError(log, "runner grpc command failed", err)
		return nil, wrapRunnerError("command", err)
	}
	return newCommandHandle(c.client, runID, stream, log), nil
}

type grpcStream interface {
	Recv() (*runnerpb.RunnerEvent, error)
}

type runHandle struct {
	client runnerpb.RunnerClient
	runID  string
	stream grpcStream
	logger pslog.Logger

	events chan schema.ExecEvent
	done   chan struct{}

	mu        sync.Mutex
	result    core.RunResult
	runErr    error
	streamErr error
}

func newRunHandle(client runnerpb.RunnerClient, runID string, stream grpcStream, logger pslog.Logger) *runHandle {
	h := &runHandle{
		client: client,
		runID:  runID,
		stream: stream,
		logger: logger,
		events: make(chan schema.ExecEvent, 256),
		done:   make(chan struct{}),
	}
	go h.consume()
	return h
}

func (h *runHandle) Events() core.EventStream {
	return &eventStream{handle: h}
}

func (h *runHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_, err := h.client.SignalSession(ctx, &runnerpb.SignalRequest{
		RunId:  h.runID,
		Signal: toPBSignal(sig),
	})
	return err
}

func (h *runHandle) Wait(ctx context.Context) (core.RunResult, error) {
	select {
	case <-h.done:
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.runErr != nil {
			return h.result, h.runErr
		}
		if h.streamErr != nil && !errors.Is(h.streamErr, io.EOF) {
			return h.result, h.streamErr
		}
		return h.result, nil
	case <-ctx.Done():
		return core.RunResult{}, ctx.Err()
	}
}

func (h *runHandle) Done() <-chan struct{} {
	return h.done
}

func (h *runHandle) Close() error {
	return nil
}

func (h *runHandle) consume() {
	defer close(h.events)
	defer close(h.done)
	for {
		msg, err := h.stream.Recv()
		if err != nil {
			if h.logger != nil {
				logGRPCError(h.logger, "runner grpc exec stream failed", err)
			}
			h.mu.Lock()
			h.streamErr = wrapRunnerError("exec", err)
			h.mu.Unlock()
			return
		}
		switch payload := msg.Payload.(type) {
		case *runnerpb.RunnerEvent_Exec:
			if payload.Exec != nil {
				if h.logger != nil {
					itemType := ""
					if payload.Exec.Item != nil {
						itemType = payload.Exec.Item.GetType().String()
					}
					h.logger.Trace("runner grpc exec event", "type", payload.Exec.GetType().String(), "item_type", itemType)
				}
				h.events <- fromPBExecEvent(payload.Exec)
			}
		case *runnerpb.RunnerEvent_Status:
			if payload.Status != nil {
				if payload.Status.State == runnerpb.RunState_RUN_STATE_STARTED {
					continue
				}
				if h.logger != nil {
					h.logger.Info("runner grpc exec finished", "state", payload.Status.State.String(), "exit_code", payload.Status.ExitCode)
				}
				h.mu.Lock()
				h.result = core.RunResult{ExitCode: int(payload.Status.ExitCode)}
				if payload.Status.State == runnerpb.RunState_RUN_STATE_FAILED {
					if payload.Status.Message != "" {
						h.runErr = errors.New(payload.Status.Message)
					} else {
						h.runErr = errors.New("runner failed")
					}
				}
				h.mu.Unlock()
			}
			return
		case *runnerpb.RunnerEvent_CommandOutput:
			// Ignore command output for core.Runner.
		default:
		}
	}
}

type eventStream struct {
	handle *runHandle
}

func (s *eventStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	select {
	case event, ok := <-s.handle.events:
		if !ok {
			s.handle.mu.Lock()
			defer s.handle.mu.Unlock()
			if s.handle.streamErr != nil && !errors.Is(s.handle.streamErr, io.EOF) {
				return schema.ExecEvent{}, s.handle.streamErr
			}
			return schema.ExecEvent{}, io.EOF
		}
		return event, nil
	case <-ctx.Done():
		return schema.ExecEvent{}, ctx.Err()
	}
}

func (s *eventStream) Close() error {
	return nil
}

type commandHandle struct {
	client runnerpb.RunnerClient
	runID  string
	stream runnerpb.Runner_RunCommandClient
	logger pslog.Logger

	outputs chan core.CommandOutput
	done    chan struct{}

	mu        sync.Mutex
	result    core.RunResult
	runErr    error
	streamErr error
}

func newCommandHandle(client runnerpb.RunnerClient, runID string, stream runnerpb.Runner_RunCommandClient, logger pslog.Logger) *commandHandle {
	h := &commandHandle{
		client:  client,
		runID:   runID,
		stream:  stream,
		logger:  logger,
		outputs: make(chan core.CommandOutput, 256),
		done:    make(chan struct{}),
	}
	go h.consume()
	return h
}

func (h *commandHandle) Outputs() core.CommandStream {
	return &commandStream{handle: h}
}

func (h *commandHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_, err := h.client.SignalSession(ctx, &runnerpb.SignalRequest{
		RunId:  h.runID,
		Signal: toPBSignal(sig),
	})
	return err
}

func (h *commandHandle) Wait(ctx context.Context) (core.RunResult, error) {
	select {
	case <-h.done:
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.runErr != nil {
			return h.result, h.runErr
		}
		if h.streamErr != nil && !errors.Is(h.streamErr, io.EOF) {
			return h.result, h.streamErr
		}
		return h.result, nil
	case <-ctx.Done():
		return core.RunResult{}, ctx.Err()
	}
}

func (h *commandHandle) Done() <-chan struct{} {
	return h.done
}

func (h *commandHandle) Close() error {
	return nil
}

func (h *commandHandle) consume() {
	defer close(h.outputs)
	defer close(h.done)
	for {
		msg, err := h.stream.Recv()
		if err != nil {
			if h.logger != nil {
				logGRPCError(h.logger, "runner grpc command stream failed", err)
			}
			h.mu.Lock()
			h.streamErr = wrapRunnerError("command", err)
			h.mu.Unlock()
			return
		}
		switch payload := msg.Payload.(type) {
		case *runnerpb.RunnerEvent_CommandOutput:
			if payload.CommandOutput != nil {
				if h.logger != nil {
					h.logger.Trace("runner grpc command output", "stream", payload.CommandOutput.Stream.String(), "text_len", len(payload.CommandOutput.Text))
				}
				h.outputs <- fromPBCommandOutput(payload.CommandOutput)
			}
		case *runnerpb.RunnerEvent_Status:
			if payload.Status != nil {
				if payload.Status.State == runnerpb.RunState_RUN_STATE_STARTED {
					continue
				}
				if h.logger != nil {
					h.logger.Trace("runner grpc command finished", "state", payload.Status.State.String(), "exit_code", payload.Status.ExitCode)
				}
				h.mu.Lock()
				h.result = core.RunResult{ExitCode: int(payload.Status.ExitCode)}
				if payload.Status.State == runnerpb.RunState_RUN_STATE_FAILED {
					if payload.Status.Message != "" {
						h.runErr = errors.New(payload.Status.Message)
					} else {
						h.runErr = errors.New("command failed")
					}
				}
				h.mu.Unlock()
			}
			return
		default:
		}
	}
}

type commandStream struct {
	handle *commandHandle
}

func (s *commandStream) Next(ctx context.Context) (core.CommandOutput, error) {
	select {
	case output, ok := <-s.handle.outputs:
		if !ok {
			s.handle.mu.Lock()
			defer s.handle.mu.Unlock()
			if s.handle.streamErr != nil && !errors.Is(s.handle.streamErr, io.EOF) {
				return core.CommandOutput{}, s.handle.streamErr
			}
			return core.CommandOutput{}, io.EOF
		}
		return output, nil
	case <-ctx.Done():
		return core.CommandOutput{}, ctx.Err()
	}
}

func (s *commandStream) Close() error {
	return nil
}

func logGRPCError(log pslog.Logger, msg string, err error) {
	if log == nil || err == nil {
		return
	}
	if st, ok := status.FromError(err); ok {
		log.Warn(msg, "err", err, "code", st.Code().String(), "message", st.Message())
		return
	}
	log.Warn(msg, "err", err)
}

func wrapRunnerError(op string, err error) error {
	if err == nil {
		return nil
	}
	var existing *core.RunnerError
	if errors.As(err, &existing) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return core.NewRunnerError(core.RunnerErrorCanceled, op, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return core.NewRunnerError(core.RunnerErrorTimeout, op, err)
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unauthenticated:
			return core.NewRunnerError(core.RunnerErrorUnauthorized, op, err)
		case codes.PermissionDenied:
			return core.NewRunnerError(core.RunnerErrorPermissionDenied, op, err)
		case codes.Unavailable:
			return core.NewRunnerError(core.RunnerErrorUnavailable, op, err)
		case codes.DeadlineExceeded:
			return core.NewRunnerError(core.RunnerErrorTimeout, op, err)
		case codes.Canceled:
			return core.NewRunnerError(core.RunnerErrorCanceled, op, err)
		default:
			return core.NewRunnerError(core.RunnerErrorUnknown, op, err)
		}
	}
	return core.NewRunnerError(core.RunnerErrorUnknown, op, err)
}

func newRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "run-unknown"
	}
	return hex.EncodeToString(buf[:])
}

func toPBSignal(sig core.ProcessSignal) runnerpb.ProcessSignal {
	switch sig {
	case core.ProcessSignalHUP:
		return runnerpb.ProcessSignal_PROCESS_SIGNAL_HUP
	case core.ProcessSignalTERM:
		return runnerpb.ProcessSignal_PROCESS_SIGNAL_TERM
	case core.ProcessSignalKILL:
		return runnerpb.ProcessSignal_PROCESS_SIGNAL_KILL
	default:
		return runnerpb.ProcessSignal_PROCESS_SIGNAL_UNSPECIFIED
	}
}

func fromPBCommandOutput(output *runnerpb.CommandOutput) core.CommandOutput {
	if output == nil {
		return core.CommandOutput{}
	}
	stream := core.CommandStreamStdout
	if output.Stream == runnerpb.StreamKind_STREAM_KIND_STDERR {
		stream = core.CommandStreamStderr
	}
	return core.CommandOutput{Stream: stream, Text: output.Text}
}
