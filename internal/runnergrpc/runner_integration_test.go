package runnergrpc

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
)

func TestExecStream(t *testing.T) {
	runner := &fakeRunner{
		events: []schema.ExecEvent{
			{Type: schema.EventThreadStarted, ThreadID: "thread-1"},
			{Type: schema.EventItemCompleted, Item: &schema.ItemEvent{Type: schema.ItemAgentMessage, Text: "hello"}},
			{Type: schema.EventTurnCompleted},
		},
	}
	client, cleanup := startTestServer(t, runner)
	defer cleanup()

	handle, err := client.Run(context.Background(), core.RunRequest{
		Prompt: "hello",
		JSON:   true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	stream := handle.Events()
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.Type != schema.EventThreadStarted {
		t.Fatalf("expected thread.started, got %s", event.Type)
	}
	event, err = stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(2): %v", err)
	}
	if event.Item == nil || event.Item.Text != "hello" {
		t.Fatalf("unexpected item event: %+v", event)
	}
	_, _ = stream.Next(context.Background())

	if runner.lastReq.Prompt != "hello" {
		t.Fatalf("expected prompt to be captured")
	}

	result, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecResumeUsesSessionID(t *testing.T) {
	runner := &fakeRunner{
		events: []schema.ExecEvent{
			{Type: schema.EventThreadStarted, ThreadID: "thread-1"},
			{Type: schema.EventTurnCompleted},
		},
	}
	client, cleanup := startTestServer(t, runner)
	defer cleanup()

	_, err := client.Run(context.Background(), core.RunRequest{
		Prompt:          "resume",
		JSON:            true,
		ResumeSessionID: "thread-99",
	})
	if err != nil {
		t.Fatalf("Run resume: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return runner.lastReq.ResumeSessionID != ""
	})
	if runner.lastReq.ResumeSessionID != "thread-99" {
		t.Fatalf("expected resume session id to be passed, got %q", runner.lastReq.ResumeSessionID)
	}
}

func TestExecSignal(t *testing.T) {
	runner := &fakeRunner{
		events: []schema.ExecEvent{
			{Type: schema.EventThreadStarted, ThreadID: "thread-1"},
		},
		block: make(chan struct{}),
	}
	client, cleanup := startTestServer(t, runner)
	defer cleanup()

	handle, err := client.Run(context.Background(), core.RunRequest{
		Prompt: "signal",
		JSON:   true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return runner.lastReq.Prompt != ""
	})
	if err := handle.Signal(context.Background(), core.ProcessSignalTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return runner.lastSignal != ""
	})
	if runner.lastSignal != core.ProcessSignalTERM {
		t.Fatalf("expected signal to be recorded")
	}
	close(runner.block)
}

func TestRunCommand(t *testing.T) {
	runner := &fakeRunner{}
	client, cleanup := startTestServer(t, runner)
	defer cleanup()

	handle, err := client.RunCommand(context.Background(), core.RunCommandRequest{
		Command:  "echo hello",
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	stream := handle.Outputs()
	output, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if output.Text != "hello" {
		t.Fatalf("unexpected output: %q", output.Text)
	}
	_, _ = handle.Wait(context.Background())
}

func TestRunCommandSignalStopsProcess(t *testing.T) {
	runner := &fakeRunner{}
	client, cleanup := startTestServer(t, runner)
	defer cleanup()

	handle, err := client.RunCommand(context.Background(), core.RunCommandRequest{
		Command:  "echo ready; sleep 5",
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	stream := handle.Outputs()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if output.Text != "ready" {
		t.Fatalf("unexpected output: %q", output.Text)
	}

	if err := handle.Signal(context.Background(), core.ProcessSignalTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := handle.Wait(context.Background())
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("command did not stop after SIGTERM")
	}
}

func startTestServer(t *testing.T, runner core.Runner) (*Client, func()) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "runner.sock")
	server := NewServer(Config{SocketPath: socket}, runner)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	if err := waitForSocket(socket, 2*time.Second); err != nil {
		cancel()
		t.Fatalf("socket not ready: %v", err)
	}
	client, err := Dial(context.Background(), socket)
	if err != nil {
		cancel()
		t.Fatalf("Dial: %v", err)
	}
	return client, func() {
		_ = client.Close()
		cancel()
		<-errCh
	}
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

func waitFor(t *testing.T, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

type fakeRunner struct {
	mu         sync.Mutex
	lastReq    core.RunRequest
	lastSignal core.ProcessSignal
	events     []schema.ExecEvent
	block      chan struct{}
}

func (f *fakeRunner) Run(ctx context.Context, req core.RunRequest) (core.RunHandle, error) {
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()
	stream := &fakeStream{
		events: f.events,
		block:  f.block,
		done:   make(chan struct{}),
	}
	return &fakeHandle{
		stream: stream,
		signalFn: func(sig core.ProcessSignal) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.lastSignal = sig
		},
	}, nil
}

func (f *fakeRunner) RunCommand(context.Context, core.RunCommandRequest) (core.CommandHandle, error) {
	return nil, errors.New("RunCommand not used")
}

type fakeHandle struct {
	stream   *fakeStream
	signalFn func(core.ProcessSignal)
}

func (h *fakeHandle) Events() core.EventStream {
	return h.stream
}

func (h *fakeHandle) Signal(ctx context.Context, sig core.ProcessSignal) error {
	_ = ctx
	if h.signalFn != nil {
		h.signalFn(sig)
	}
	return nil
}

func (h *fakeHandle) Wait(ctx context.Context) (core.RunResult, error) {
	select {
	case <-h.stream.done:
		return core.RunResult{ExitCode: 0}, nil
	case <-ctx.Done():
		return core.RunResult{}, ctx.Err()
	}
}

func (h *fakeHandle) Close() error {
	h.stream.closeDone()
	return nil
}

type fakeStream struct {
	events []schema.ExecEvent
	index  int
	block  chan struct{}
	done   chan struct{}
}

func (s *fakeStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	if ctx.Err() != nil {
		return schema.ExecEvent{}, ctx.Err()
	}
	if s.index >= len(s.events) {
		if s.block != nil {
			select {
			case <-s.block:
			case <-ctx.Done():
				return schema.ExecEvent{}, ctx.Err()
			}
		}
		s.closeDone()
		return schema.ExecEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *fakeStream) Close() error {
	s.closeDone()
	return nil
}

func (s *fakeStream) closeDone() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}
