package command

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

func TestHandleShellAuditLog(t *testing.T) {
	capture := newLogCapture(t)
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		VerboseFields: true,
		MinLevel:      pslog.DebugLevel,
	})
	ctx := pslog.ContextWithLogger(context.Background(), logger)

	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	repoPath := "/repos/demo"

	svc := &fakeService{
		listTabsFn: func(_ context.Context, req schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs: []schema.TabSnapshot{
					{ID: tabID, Repo: schema.RepoRef{Name: "demo", Path: repoPath}},
				},
			}, nil
		},
		appendOutputFn: func(_ context.Context, _ schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			return schema.AppendOutputResponse{}, nil
		},
	}

	handler := NewHandler(svc, fakeRunnerProvider{resp: core.RunnerResponse{Runner: &fakeRunner{}}}, HandlerConfig{})
	handled, err := handler.Handle(ctx, user, tabID, "!echo hi")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled command")
	}

	entries := capture.Entries()
	if !hasAuditCommand(entries, "shell", "echo hi", repoPath) {
		t.Fatalf("expected audit log for shell command, got %d entries", len(entries))
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

func hasAuditCommand(entries []logEntry, commandType, command, workdir string) bool {
	for _, entry := range entries {
		if entry.Level != "debug" || entry.Message != "audit command" {
			continue
		}
		if entry.Fields == nil {
			continue
		}
		if entry.Fields["command_type"] != commandType {
			continue
		}
		if entry.Fields["command"] != command {
			continue
		}
		if workdir != "" && entry.Fields["workdir"] != workdir {
			continue
		}
		return true
	}
	return false
}
