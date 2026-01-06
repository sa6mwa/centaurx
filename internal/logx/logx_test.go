package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

func TestWithRepoAddsFields(t *testing.T) {
	capture := &logCapture{}
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		MinLevel:      pslog.InfoLevel,
		VerboseFields: true,
	})
	log := WithRepo(logger, schema.RepoRef{Name: "demo"})
	log.Info("hello")

	entry := capture.firstEntry(t)
	if entry["repo"] != "demo" {
		t.Fatalf("expected repo field, got %+v", entry)
	}
	if _, ok := entry["repo_path"]; ok {
		t.Fatalf("did not expect repo_path for name-only repo")
	}
}

func TestWithRepoAddsPath(t *testing.T) {
	capture := &logCapture{}
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		MinLevel:      pslog.InfoLevel,
		VerboseFields: true,
	})
	log := WithRepo(logger, schema.RepoRef{Name: "demo", Path: "/repos/demo"})
	log.Info("hello")

	entry := capture.firstEntry(t)
	if entry["repo_path"] != "/repos/demo" {
		t.Fatalf("expected repo_path field, got %+v", entry)
	}
}

func TestWithUserTabAddsFields(t *testing.T) {
	capture := &logCapture{}
	logger := pslog.NewWithOptions(capture, pslog.Options{
		Mode:          pslog.ModeStructured,
		NoColor:       true,
		MinLevel:      pslog.InfoLevel,
		VerboseFields: true,
	})
	ctx := pslog.ContextWithLogger(context.Background(), logger)
	log := WithUserTab(ctx, "alice", "tab1")
	log.Info("hello")

	entry := capture.firstEntry(t)
	if entry["user"] != "alice" {
		t.Fatalf("expected user field, got %+v", entry)
	}
	if entry["tab"] != "tab1" {
		t.Fatalf("expected tab field, got %+v", entry)
	}
}

type logCapture struct {
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

func (c *logCapture) firstEntry(t *testing.T) map[string]any {
	t.Helper()
	data := c.buf.Bytes()
	idx := bytes.IndexByte(data, '\n')
	if idx == -1 {
		idx = len(data)
	}
	line := bytes.TrimSpace(data[:idx])
	entry := map[string]any{}
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("parse log entry: %v", err)
	}
	return entry
}
