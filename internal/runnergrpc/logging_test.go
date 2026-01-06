package runnergrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

func TestRunnerLoggingFlow(t *testing.T) {
	capture := newLogCapture(t)
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		VerboseFields: true,
		MinLevel:      pslog.TraceLevel,
	})
	ctx := pslog.ContextWithLogger(context.Background(), logger)
	t.Cleanup(func() {
		if testing.Verbose() {
			capture.Dump(t)
		}
	})

	runner := &fakeRunner{
		events: []schema.ExecEvent{
			{Type: schema.EventThreadStarted, ThreadID: "thread-1"},
			{Type: schema.EventItemCompleted, Item: &schema.ItemEvent{Type: schema.ItemAgentMessage, Text: "hello"}},
			{Type: schema.EventTurnCompleted},
		},
	}
	client, cleanup := startTestServerWithContext(ctx, t, runner)
	defer cleanup()

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	handle, err := client.Run(runCtx, core.RunRequest{
		Prompt: "hello",
		JSON:   true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	stream := handle.Events()
	for {
		_, err := stream.Next(runCtx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Events: %v", err)
		}
	}
	if _, err := handle.Wait(runCtx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	cmdHandle, err := client.RunCommand(runCtx, core.RunCommandRequest{
		Command:  "printf 'out\\n'; printf 'err\\n' 1>&2",
		UseShell: true,
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	var stdoutSeen bool
	var stderrSeen bool
	outStream := cmdHandle.Outputs()
	for {
		out, err := outStream.Next(runCtx)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Outputs: %v", err)
		}
		switch out.Stream {
		case core.CommandStreamStdout:
			stdoutSeen = true
		case core.CommandStreamStderr:
			stderrSeen = true
		}
	}
	if _, err := cmdHandle.Wait(runCtx); err != nil {
		t.Fatalf("Command Wait: %v", err)
	}
	if !stdoutSeen || !stderrSeen {
		t.Fatalf("expected stdout and stderr output (stdout=%t stderr=%t)", stdoutSeen, stderrSeen)
	}

	entries := capture.Entries()
	requireLog(t, entries, "info", "runner grpc exec start")
	requireLog(t, entries, "debug", "runner grpc exec request")
	requireLog(t, entries, "trace", "runner grpc exec event")
	requireLog(t, entries, "info", "runner grpc exec finished")
	requireLog(t, entries, "trace", "runner grpc command start")
	requireLog(t, entries, "debug", "runner grpc command request")
	requireLog(t, entries, "trace", "runner grpc command output")
	requireLog(t, entries, "trace", "runner grpc command finished")
}

func startTestServerWithContext(ctx context.Context, t *testing.T, runner core.Runner) (*Client, func()) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "runner.sock")
	server := NewServer(Config{SocketPath: socket}, runner)

	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(runCtx)
	}()
	if err := waitForSocket(socket, 2*time.Second); err != nil {
		cancel()
		t.Fatalf("socket not ready: %v", err)
	}
	client, err := Dial(ctx, socket)
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

type logEntry struct {
	Level   string
	Message string
	Fields  map[string]any
	Raw     string
}

type logCapture struct {
	t     *testing.T
	mu    sync.Mutex
	buf   bytes.Buffer
	lines []string
}

func newLogCapture(t *testing.T) *logCapture {
	t.Helper()
	return &logCapture{t: t}
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.buf.Write(p)
	for {
		data := c.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx == -1 {
			break
		}
		line := string(data[:idx])
		c.lines = append(c.lines, line)
		c.buf.Next(idx + 1)
	}
	return len(p), nil
}

func (c *logCapture) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len() > 0 {
		c.lines = append(c.lines, c.buf.String())
		c.buf.Reset()
	}
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

func (c *logCapture) Entries() []logEntry {
	lines := c.Lines()
	entries := make([]logEntry, 0, len(lines))
	for _, line := range lines {
		entries = append(entries, parseLogEntry(line))
	}
	return entries
}

func (c *logCapture) Dump(t *testing.T) {
	t.Helper()
	for _, line := range c.Lines() {
		t.Log(line)
	}
}

func parseLogEntry(line string) logEntry {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return logEntry{Raw: line}
	}
	level := ""
	if value, ok := payload["level"].(string); ok {
		level = value
	} else if value, ok := payload["lvl"].(string); ok {
		level = value
	}
	message := ""
	if value, ok := payload["message"].(string); ok {
		message = value
	} else if value, ok := payload["msg"].(string); ok {
		message = value
	}
	return logEntry{Level: level, Message: message, Fields: payload, Raw: line}
}

func requireLog(t *testing.T, entries []logEntry, level, message string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Level == level && entry.Message == message {
			return
		}
	}
	t.Fatalf("expected log level=%q message=%q; got %d entries", level, message, len(entries))
}
