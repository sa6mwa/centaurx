package core

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

func TestSendPromptAuditLogExec(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}

	capture := newLogCapture(t)
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		VerboseFields: true,
		MinLevel:      pslog.DebugLevel,
	})
	ctx := pslog.ContextWithLogger(context.Background(), logger)

	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: workedRunner{}},
		RepoResolver:   resolver,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(ctx, schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForTabIdle(t, svc, user, tabResp.Tab.ID)
	entries := capture.Entries()
	if !hasAuditCommand(entries, "codex", "codex exec --json") {
		t.Fatalf("expected audit log for codex exec, got %d entries", len(entries))
	}
}

func TestSendPromptAuditLogResume(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}

	capture := newLogCapture(t)
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		VerboseFields: true,
		MinLevel:      pslog.DebugLevel,
	})
	ctx := pslog.ContextWithLogger(context.Background(), logger)

	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: workedRunner{}},
		RepoResolver:   resolver,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(ctx, schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	svcImpl := svc.(*service)
	svcImpl.mu.Lock()
	if state := svcImpl.userTabs[user]; state != nil {
		if tab := state.tabs[tabResp.Tab.ID]; tab != nil {
			tab.SessionID = schema.SessionID("sess-1")
		}
	}
	svcImpl.mu.Unlock()

	if _, err := svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "resume",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForTabIdle(t, svc, user, tabResp.Tab.ID)
	entries := capture.Entries()
	if !hasAuditCommand(entries, "codex", "codex exec resume sess-1 --json") {
		t.Fatalf("expected audit log for codex resume, got %d entries", len(entries))
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

func hasAuditCommand(entries []logEntry, commandType, command string) bool {
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
		return true
	}
	return false
}

func waitForTabIdle(t *testing.T, svc Service, user schema.UserID, tabID schema.TabID) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp, err := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
		if err != nil {
			t.Fatalf("list tabs: %v", err)
		}
		for _, tab := range resp.Tabs {
			if tab.ID == tabID && tab.Status == schema.TabStatusIdle {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp, _ := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
	t.Fatalf("timed out waiting for tab idle: %v", resp.Tabs)
}
