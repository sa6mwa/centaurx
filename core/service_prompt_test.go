package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/schema"
)

func TestSendPromptRunnerProviderErrorAppendsError(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{err: errors.New("runner down")},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	_, err = svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected send prompt to fail")
	}
	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := strings.Join(buf.Buffer.Lines, "\n")
	if !strings.Contains(lines, "Starting codex exec") {
		t.Fatalf("expected start line, got %v", buf.Buffer.Lines)
	}
	if !strings.Contains(lines, "error:") {
		t.Fatalf("expected error line, got %v", buf.Buffer.Lines)
	}
}

func TestSendPromptRunnerErrorAppendsError(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: errorRunner{err: errors.New("run failed")}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	_, err = svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected send prompt to fail")
	}
	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := strings.Join(buf.Buffer.Lines, "\n")
	if !strings.Contains(lines, "Starting codex exec") {
		t.Fatalf("expected start line, got %v", buf.Buffer.Lines)
	}
	if !strings.Contains(lines, "error:") {
		t.Fatalf("expected error line, got %v", buf.Buffer.Lines)
	}
}

func TestSendPromptRunnerUnauthenticatedAddsHint(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{err: NewRunnerError(RunnerErrorUnauthorized, "exec", errors.New("unauthorized"))},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	_, err = svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected send prompt to fail")
	}
	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := strings.Join(buf.Buffer.Lines, "\n")
	if !strings.Contains(lines, "runner authentication failed") {
		t.Fatalf("expected auth error hint, got %v", buf.Buffer.Lines)
	}
	if !strings.Contains(lines, "codex login") {
		t.Fatalf("expected login hint, got %v", buf.Buffer.Lines)
	}
}

func TestSendPromptDetachesRunContext(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	block := make(chan struct{})
	runner := &capturingRunner{stream: &blockingStream{block: block}, ready: make(chan struct{})}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: runner},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	select {
	case <-runner.ready:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runner")
	}
	cancel()
	if runCtx := runner.Context(); runCtx == nil || runCtx.Err() != nil {
		t.Fatalf("expected detached run context, got err=%v", runCtx.Err())
	}
	close(block)
}

func TestStopSessionSignalsCommandRuns(t *testing.T) {
	origSleep := stopSleep
	sleepStarted := make(chan struct{})
	unblockSleep := make(chan struct{})
	stopSleep = func(time.Duration) {
		select {
		case <-sleepStarted:
		default:
			close(sleepStarted)
		}
		<-unblockSleep
	}
	defer func() { stopSleep = origSleep }()

	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	tracker, ok := svc.(CommandTracker)
	if !ok {
		t.Fatalf("service does not implement CommandTracker")
	}
	cmd1 := newSignalCommandHandle()
	cmd2 := newSignalCommandHandle()
	tracker.RegisterCommand(context.Background(), user, tabResp.Tab.ID, cmd1, nil)
	tracker.RegisterCommand(context.Background(), user, tabResp.Tab.ID, cmd2, nil)

	runHandle := newSignalRunHandle()
	svcImpl, ok := svc.(*service)
	if !ok {
		t.Fatalf("expected *service implementation")
	}
	svcImpl.mu.Lock()
	state := svcImpl.getOrCreateUserStateLocked(user)
	if tab := state.tabs[tabResp.Tab.ID]; tab != nil {
		tab.Run = runHandle
		tab.RunCancel = func() {}
	}
	svcImpl.mu.Unlock()

	done := make(chan struct{})
	go func() {
		if _, err := svc.StopSession(context.Background(), schema.StopSessionRequest{UserID: user, TabID: tabResp.Tab.ID}); err != nil {
			t.Errorf("stop session: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("stop session blocked")
	}

	waitForSignal(t, runHandle, ProcessSignalTERM)
	waitForSignal(t, cmd1, ProcessSignalTERM)
	waitForSignal(t, cmd2, ProcessSignalTERM)
	runHandle.markDone()
	cmd1.markDone()
	cmd2.markDone()

	select {
	case <-sleepStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("expected stop sleep to start")
	}

	close(unblockSleep)

	assertNoSignal(t, runHandle, ProcessSignalKILL)
	assertNoSignal(t, cmd1, ProcessSignalKILL)
	assertNoSignal(t, cmd2, ProcessSignalKILL)
}

func TestCloseTabSkipsKillWhenDone(t *testing.T) {
	origSleep := stopSleep
	sleepStarted := make(chan struct{})
	unblockSleep := make(chan struct{})
	stopSleep = func(time.Duration) {
		select {
		case <-sleepStarted:
		default:
			close(sleepStarted)
		}
		<-unblockSleep
	}
	defer func() { stopSleep = origSleep }()

	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	tracker, ok := svc.(CommandTracker)
	if !ok {
		t.Fatalf("service does not implement CommandTracker")
	}
	cmd1 := newSignalCommandHandle()
	cmd2 := newSignalCommandHandle()
	tracker.RegisterCommand(context.Background(), user, tabResp.Tab.ID, cmd1, nil)
	tracker.RegisterCommand(context.Background(), user, tabResp.Tab.ID, cmd2, nil)

	runHandle := newSignalRunHandle()
	svcImpl, ok := svc.(*service)
	if !ok {
		t.Fatalf("expected *service implementation")
	}
	svcImpl.mu.Lock()
	state := svcImpl.getOrCreateUserStateLocked(user)
	if tab := state.tabs[tabResp.Tab.ID]; tab != nil {
		tab.Run = runHandle
		tab.RunCancel = func() {}
	}
	svcImpl.mu.Unlock()

	if _, err := svc.CloseTab(context.Background(), schema.CloseTabRequest{UserID: user, TabID: tabResp.Tab.ID}); err != nil {
		t.Fatalf("close tab: %v", err)
	}

	waitForSignal(t, runHandle, ProcessSignalTERM)
	waitForSignal(t, cmd1, ProcessSignalTERM)
	waitForSignal(t, cmd2, ProcessSignalTERM)
	runHandle.markDone()
	cmd1.markDone()
	cmd2.markDone()

	select {
	case <-sleepStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("expected stop sleep to start")
	}

	close(unblockSleep)

	assertNoSignal(t, runHandle, ProcessSignalKILL)
	assertNoSignal(t, cmd1, ProcessSignalKILL)
	assertNoSignal(t, cmd2, ProcessSignalKILL)
}

func TestSendPromptRunnerSocketHint(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{err: NewRunnerError(RunnerErrorContainerSocket, "socket wait", errors.New("socket wait failed"))},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	_, err = svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected send prompt to fail")
	}
	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := strings.Join(buf.Buffer.Lines, "\n")
	if !strings.Contains(lines, "runner socket did not become ready") {
		t.Fatalf("expected socket error hint, got %v", buf.Buffer.Lines)
	}
	if !strings.Contains(lines, "runner logs") {
		t.Fatalf("expected runner logs hint, got %v", buf.Buffer.Lines)
	}
}

func TestSendPromptRunnerContainerStartHint(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{err: NewRunnerError(RunnerErrorContainerStart, "container start", errors.New("boom"))},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	_, err = svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected send prompt to fail")
	}
	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := strings.Join(buf.Buffer.Lines, "\n")
	if !strings.Contains(lines, "runner container failed to start") {
		t.Fatalf("expected container start hint, got %v", buf.Buffer.Lines)
	}
	if !strings.Contains(lines, "container runtime logs") {
		t.Fatalf("expected runtime logs hint, got %v", buf.Buffer.Lines)
	}
}

func TestSendPromptEventErrorAppendsErrorLine(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: eventRunner{
			events: []schema.ExecEvent{
				{Type: schema.EventError, Message: "unexpected argument --json found"},
				{Type: schema.EventTurnCompleted},
			},
			exitCode: 2,
		}},
		RepoResolver: resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		if containsLine(buf.Buffer.Lines, "error: unexpected argument --json found") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	buf, _ := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	t.Fatalf("expected error line, got %v", buf.Buffer.Lines)
}

func TestSendPromptTurnFailedAppendsLine(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: eventRunner{
			events: []schema.ExecEvent{
				{Type: schema.EventTurnFailed, Error: &schema.ErrorEvent{Message: "mock failure: simulated turn error"}},
			},
			exitCode: 1,
		}},
		RepoResolver: resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		if containsLine(buf.Buffer.Lines, "turn failed: mock failure: simulated turn error") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	buf, _ := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID})
	t.Fatalf("expected turn failed line, got %v", buf.Buffer.Lines)
}

func TestSendPromptCapturesSessionID(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: eventRunner{
			events: []schema.ExecEvent{
				{Type: schema.EventThreadStarted, ThreadID: "thread-123"},
				{Type: schema.EventTurnCompleted},
			},
			exitCode: 0,
		}},
		RepoResolver: resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp, err := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
		if err != nil {
			t.Fatalf("list tabs: %v", err)
		}
		if len(resp.Tabs) > 0 && resp.Tabs[0].SessionID == "thread-123" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp, _ := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
	t.Fatalf("expected session id to be captured, got %v", resp.Tabs)
}

func TestSendPromptAddsWorkedForSeparator(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: workedRunner{}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID, Limit: 200})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		lines := buf.Buffer.Lines
		workedIdx := -1
		msgIdx := -1
		for i, line := range lines {
			if strings.Contains(line, "Worked for") {
				workedIdx = i
			}
			if strings.Contains(line, "final response") {
				msgIdx = i
			}
		}
		if workedIdx >= 0 && msgIdx >= 0 {
			if workedIdx >= msgIdx {
				t.Fatalf("expected worked-for line before agent message, got worked=%d msg=%d lines=%v", workedIdx, msgIdx, lines)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for worked-for line")
}

func TestSendPromptTracksUsage(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	usage := schema.TurnUsage{InputTokens: 1200, OutputTokens: 300}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: usageRunner{usage: usage}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp, err := svc.GetTabUsage(context.Background(), schema.GetTabUsageRequest{UserID: user, TabID: tabResp.Tab.ID})
		if err != nil {
			t.Fatalf("get usage: %v", err)
		}
		if resp.Usage != nil {
			if resp.Usage.InputTokens != usage.InputTokens || resp.Usage.OutputTokens != usage.OutputTokens {
				t.Fatalf("unexpected usage: %+v", resp.Usage)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for usage")
}

func TestSendPromptAddsExecStartSummary(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	gitRunner := &gitInfoRunner{
		outputs: map[string][]string{
			"git rev-parse --abbrev-ref HEAD": {"main"},
			"git remote -v": {
				"origin git@github.com:sa6mwa/centaurx.git (fetch)",
				"origin git@github.com:sa6mwa/centaurx.git (push)",
			},
			"git status --short": {"M BACKLOG.md", "M testserver.go"},
		},
	}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: gitRunner},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID, Limit: 200})
	if err != nil {
		t.Fatalf("get buffer: %v", err)
	}
	lines := buf.Buffer.Lines
	if !containsLine(lines, "Starting codex exec") {
		t.Fatalf("expected start header, got %v", lines)
	}
	if !containsLine(lines, "Repository:") || !containsLine(lines, string(repo.Name)) {
		t.Fatalf("expected repository line, got %v", lines)
	}
	if !containsLine(lines, "Branch:") || !containsLine(lines, "main") {
		t.Fatalf("expected branch line, got %v", lines)
	}
	remoteLines := filterLines(lines, "Remote:")
	if len(remoteLines) != 1 {
		t.Fatalf("expected one remote line, got %v", remoteLines)
	}
	if strings.Contains(remoteLines[0], "(push)") || strings.Contains(remoteLines[0], "(fetch)") {
		t.Fatalf("expected remote to be deduped, got %v", remoteLines)
	}
	statusLine, ok := findLineWithPrefix(lines, "Git status:")
	if !ok || !strings.Contains(statusLine, "BACKLOG.md") {
		t.Fatalf("expected git status line, got %v", lines)
	}
	indent := strings.Repeat(" ", len("Git status:")+1)
	foundSecond := false
	for _, line := range lines {
		if strings.HasPrefix(line, indent) && strings.Contains(line, "testserver.go") {
			foundSecond = true
			break
		}
	}
	if !foundSecond {
		t.Fatalf("expected aligned git status continuation, got %v", lines)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp, err := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
		if err != nil {
			t.Fatalf("list tabs: %v", err)
		}
		if len(resp.Tabs) > 0 && resp.Tabs[0].Status == schema.TabStatusIdle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	resp, _ := svc.ListTabs(context.Background(), schema.ListTabsRequest{UserID: user})
	t.Fatalf("timed out waiting for tab idle: %v", resp.Tabs)
}

func TestCommandExecutionTerseLimit(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: commandRunner{outputLines: 8}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	ctx := sessionprefs.WithContext(context.Background(), sessionprefs.New())
	if _, err := svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	var commandLines []string
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID, Limit: 200})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		lines := filterLinesWithPrefix(buf.Buffer.Lines, schema.CommandMarker)
		if len(lines) > 0 {
			commandLines = lines
			if len(lines) == 5 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(commandLines) != 5 {
		t.Fatalf("expected 5 command lines, got %d (%v)", len(commandLines), commandLines)
	}
}

func TestCommandExecutionFullOutput(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: commandRunner{outputLines: 8}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	prefs := sessionprefs.New()
	prefs.FullCommandOutput = true
	ctx := sessionprefs.WithContext(context.Background(), prefs)
	if _, err := svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	var commandLines []string
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID, Limit: 200})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		lines := filterLinesWithPrefix(buf.Buffer.Lines, schema.CommandMarker)
		if len(lines) >= 10 {
			commandLines = lines
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(commandLines) < 10 {
		t.Fatalf("expected full command output (>=10 lines), got %d (%v)", len(commandLines), commandLines)
	}
}

func TestCommandExecutionDedupesCommandLineAcrossEvents(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: filepath.Join(repoRoot, "demo")}
	resolver := fakeRepoResolver{repo: repo}
	exitCode := 0
	events := []schema.ExecEvent{
		{
			Type: schema.EventItemStarted,
			Item: &schema.ItemEvent{
				ID:      "cmd-1",
				Type:    schema.ItemCommandExecution,
				Command: "/bin/sh -lc 'ls -la'",
			},
		},
		{
			Type: schema.EventItemCompleted,
			Item: &schema.ItemEvent{
				ID:               "cmd-1",
				Type:             schema.ItemCommandExecution,
				Command:          "/bin/sh -lc 'ls -la'",
				AggregatedOutput: "total 16\nfile1\n",
				ExitCode:         &exitCode,
			},
		},
	}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: eventRunner{events: events}},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	ctx := sessionprefs.WithContext(context.Background(), sessionprefs.New())
	if _, err := svc.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	var lines []string
	var count int
	for time.Now().Before(deadline) {
		buf, err := svc.GetBuffer(context.Background(), schema.GetBufferRequest{UserID: user, TabID: tabResp.Tab.ID, Limit: 200})
		if err != nil {
			t.Fatalf("get buffer: %v", err)
		}
		lines = buf.Buffer.Lines
		count = 0
		for _, line := range lines {
			if strings.HasPrefix(line, schema.CommandMarker) && strings.Contains(line, "$ /bin/sh -lc 'ls -la'") {
				count++
			}
		}
		if count > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 1 {
		t.Fatalf("expected one command line, got %d (%v)", count, lines)
	}
}

func TestSendPromptUsesComputedRepoPath(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := t.TempDir()
	repo := schema.RepoRef{Name: "demo", Path: "/wrong/path"}
	resolver := fakeRepoResolver{repo: repo}
	runner := &captureRunRunner{}
	svc, err := NewService(schema.ServiceConfig{RepoRoot: repoRoot, StateDir: stateDir}, ServiceDeps{
		RunnerProvider: fakeRunnerProvider{runner: runner},
		RepoResolver:   resolver,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	user := schema.UserID("alice")
	tabResp, err := svc.CreateTab(context.Background(), schema.CreateTabRequest{
		UserID:     user,
		RepoName:   repo.Name,
		CreateRepo: false,
	})
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if _, err := svc.SendPrompt(context.Background(), schema.SendPromptRequest{
		UserID: user,
		TabID:  tabResp.Tab.ID,
		Prompt: "hello",
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	want := filepath.Join(repoRoot, "alice", "demo")
	if runner.lastRun.WorkingDir != want {
		t.Fatalf("expected working dir %q, got %q", want, runner.lastRun.WorkingDir)
	}
}

type fakeRepoResolver struct {
	repo schema.RepoRef
}

func (f fakeRepoResolver) CreateRepo(context.Context, CreateRepoRequest) (CreateRepoResponse, error) {
	return CreateRepoResponse{Repo: f.repo}, nil
}

func (f fakeRepoResolver) ResolveRepo(context.Context, ResolveRepoRequest) (ResolveRepoResponse, error) {
	return ResolveRepoResponse{Repo: f.repo}, nil
}

func (f fakeRepoResolver) ListRepos(context.Context, ListReposRequest) (ListReposResponse, error) {
	return ListReposResponse{Repos: []schema.RepoRef{f.repo}}, nil
}

func (f fakeRepoResolver) OpenOrCloneURL(context.Context, OpenOrCloneRequest) (OpenOrCloneResponse, error) {
	return OpenOrCloneResponse{Repo: f.repo, Created: true}, nil
}

type fakeRunnerProvider struct {
	err    error
	runner Runner
	info   RunnerInfo
}

func (f fakeRunnerProvider) RunnerFor(context.Context, RunnerRequest) (RunnerResponse, error) {
	if f.err != nil {
		return RunnerResponse{}, f.err
	}
	if f.runner == nil {
		f.runner = errorRunner{err: errors.New("runner missing")}
	}
	return RunnerResponse{Runner: f.runner, Info: f.info}, nil
}

func (f fakeRunnerProvider) CloseTab(context.Context, RunnerCloseRequest) error {
	return nil
}

func (f fakeRunnerProvider) CloseAll(context.Context) error {
	return nil
}

type errorRunner struct {
	err error
}

func (e errorRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	if e.err == nil {
		return nil, errors.New("run failed")
	}
	return nil, e.err
}

func (e errorRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	if e.err == nil {
		return nil, errors.New("command failed")
	}
	return nil, e.err
}

type workedRunner struct{}

func (workedRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	return &workedHandle{}, nil
}

func (workedRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

type usageRunner struct {
	usage schema.TurnUsage
}

func (u usageRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	return &usageHandle{usage: u.usage}, nil
}

func (usageRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

type captureRunRunner struct {
	lastRun RunRequest
}

func (r *captureRunRunner) Run(_ context.Context, req RunRequest) (RunHandle, error) {
	r.lastRun = req
	return &workedHandle{}, nil
}

func (*captureRunRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

type capturingRunner struct {
	stream *blockingStream
	ready  chan struct{}
	mu     sync.Mutex
	ctx    context.Context
}

func (r *capturingRunner) Run(ctx context.Context, _ RunRequest) (RunHandle, error) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
	close(r.ready)
	return &blockingHandle{stream: r.stream}, nil
}

func (r *capturingRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

func (r *capturingRunner) Context() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctx
}

type blockingHandle struct {
	stream *blockingStream
}

func (h *blockingHandle) Events() EventStream { return h.stream }
func (h *blockingHandle) Signal(context.Context, ProcessSignal) error {
	return nil
}
func (h *blockingHandle) Wait(context.Context) (RunResult, error) {
	return RunResult{ExitCode: 0}, nil
}
func (h *blockingHandle) Close() error { return nil }

type blockingStream struct {
	block <-chan struct{}
}

func (s *blockingStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	select {
	case <-s.block:
		return schema.ExecEvent{}, io.EOF
	case <-ctx.Done():
		return schema.ExecEvent{}, ctx.Err()
	}
}

func (s *blockingStream) Close() error { return nil }

type signalCommandHandle struct {
	mu      sync.Mutex
	signals []ProcessSignal
	done    chan struct{}
	once    sync.Once
}

func newSignalCommandHandle() *signalCommandHandle {
	return &signalCommandHandle{done: make(chan struct{})}
}

func (h *signalCommandHandle) Outputs() CommandStream { return &emptyCommandStream{} }

func (h *signalCommandHandle) Signal(_ context.Context, sig ProcessSignal) error {
	h.mu.Lock()
	h.signals = append(h.signals, sig)
	h.mu.Unlock()
	return nil
}

func (h *signalCommandHandle) Wait(context.Context) (RunResult, error) {
	h.markDone()
	return RunResult{ExitCode: 0}, nil
}

func (h *signalCommandHandle) Close() error {
	h.markDone()
	return nil
}

func (h *signalCommandHandle) Done() <-chan struct{} { return h.done }

func (h *signalCommandHandle) markDone() {
	h.once.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
}

func (h *signalCommandHandle) Signals() []ProcessSignal {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ProcessSignal(nil), h.signals...)
}

type signalRunHandle struct {
	mu      sync.Mutex
	signals []ProcessSignal
	done    chan struct{}
	once    sync.Once
}

func newSignalRunHandle() *signalRunHandle {
	return &signalRunHandle{done: make(chan struct{})}
}

func (h *signalRunHandle) Events() EventStream { return &workedStream{} }

func (h *signalRunHandle) Signal(_ context.Context, sig ProcessSignal) error {
	h.mu.Lock()
	h.signals = append(h.signals, sig)
	h.mu.Unlock()
	return nil
}

func (h *signalRunHandle) Wait(context.Context) (RunResult, error) {
	h.markDone()
	return RunResult{ExitCode: 0}, nil
}

func (h *signalRunHandle) Close() error {
	h.markDone()
	return nil
}

func (h *signalRunHandle) Done() <-chan struct{} { return h.done }

func (h *signalRunHandle) markDone() {
	h.once.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
}

func (h *signalRunHandle) Signals() []ProcessSignal {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ProcessSignal(nil), h.signals...)
}

func waitForSignal(t *testing.T, handle interface{ Signals() []ProcessSignal }, want ProcessSignal) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		signals := handle.Signals()
		for _, sig := range signals {
			if sig == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for signal %v", want)
}

func assertNoSignal(t *testing.T, handle interface{ Signals() []ProcessSignal }, want ProcessSignal) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, sig := range handle.Signals() {
			if sig == want {
				t.Fatalf("unexpected signal %v", want)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type emptyCommandStream struct{}

func (emptyCommandStream) Next(context.Context) (CommandOutput, error) {
	return CommandOutput{}, io.EOF
}
func (emptyCommandStream) Close() error { return nil }

type workedHandle struct{}

func (h *workedHandle) Events() EventStream { return &workedStream{} }
func (h *workedHandle) Signal(context.Context, ProcessSignal) error {
	return nil
}
func (h *workedHandle) Wait(context.Context) (RunResult, error) {
	return RunResult{ExitCode: 0}, nil
}
func (h *workedHandle) Close() error { return nil }

type usageHandle struct {
	usage schema.TurnUsage
}

func (h *usageHandle) Events() EventStream { return &usageStream{usage: h.usage} }
func (h *usageHandle) Signal(context.Context, ProcessSignal) error {
	return nil
}
func (h *usageHandle) Wait(context.Context) (RunResult, error) {
	return RunResult{ExitCode: 0}, nil
}
func (h *usageHandle) Close() error { return nil }

type workedStream struct {
	idx int
}

func (s *workedStream) Next(context.Context) (schema.ExecEvent, error) {
	events := []schema.ExecEvent{
		{Type: schema.EventItemCompleted, Item: &schema.ItemEvent{Type: schema.ItemAgentMessage, Text: "final response"}},
		{Type: schema.EventTurnCompleted},
	}
	if s.idx >= len(events) {
		return schema.ExecEvent{}, io.EOF
	}
	ev := events[s.idx]
	s.idx++
	return ev, nil
}

type usageStream struct {
	idx   int
	usage schema.TurnUsage
}

func (s *usageStream) Next(context.Context) (schema.ExecEvent, error) {
	events := []schema.ExecEvent{
		{Type: schema.EventItemCompleted, Item: &schema.ItemEvent{Type: schema.ItemAgentMessage, Text: "final response"}},
		{Type: schema.EventTurnCompleted, Usage: &s.usage},
	}
	if s.idx >= len(events) {
		return schema.ExecEvent{}, io.EOF
	}
	ev := events[s.idx]
	s.idx++
	return ev, nil
}

func (s *usageStream) Close() error { return nil }

func (s *workedStream) Close() error { return nil }

func filterLinesWithPrefix(lines []string, prefix string) []string {
	if prefix == "" {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

func containsLine(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func filterLines(lines []string, needle string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return out
}

func findLineWithPrefix(lines []string, prefix string) (string, bool) {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}

type commandRunner struct {
	outputLines int
}

func (c commandRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	return &commandHandle{outputLines: c.outputLines}, nil
}

func (commandRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

type commandHandle struct {
	outputLines int
}

func (h *commandHandle) Events() EventStream {
	return &commandStream{outputLines: h.outputLines}
}
func (h *commandHandle) Signal(context.Context, ProcessSignal) error { return nil }
func (h *commandHandle) Wait(context.Context) (RunResult, error)     { return RunResult{ExitCode: 0}, nil }
func (h *commandHandle) Close() error                                { return nil }

type commandStream struct {
	outputLines int
	sent        bool
}

func (s *commandStream) Next(context.Context) (schema.ExecEvent, error) {
	if s.sent {
		return schema.ExecEvent{}, io.EOF
	}
	s.sent = true
	lines := make([]string, 0, s.outputLines)
	for i := 0; i < s.outputLines; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	exitCode := 0
	return schema.ExecEvent{
		Type: schema.EventItemCompleted,
		Item: &schema.ItemEvent{
			Type:             schema.ItemCommandExecution,
			Command:          "echo hello",
			AggregatedOutput: strings.Join(lines, "\n"),
			ExitCode:         &exitCode,
		},
	}, nil
}

func (s *commandStream) Close() error { return nil }

type gitInfoRunner struct {
	outputs map[string][]string
}

func (g *gitInfoRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	return &workedHandle{}, nil
}

func (g *gitInfoRunner) RunCommand(ctx context.Context, req RunCommandRequest) (CommandHandle, error) {
	_ = ctx
	lines := g.outputs[req.Command]
	return &staticCommandHandle{lines: lines}, nil
}

type staticCommandHandle struct {
	lines []string
}

func (h *staticCommandHandle) Outputs() CommandStream {
	return &staticCommandStream{lines: append([]string(nil), h.lines...)}
}
func (h *staticCommandHandle) Signal(context.Context, ProcessSignal) error { return nil }
func (h *staticCommandHandle) Wait(context.Context) (RunResult, error) {
	return RunResult{ExitCode: 0}, nil
}
func (h *staticCommandHandle) Close() error { return nil }

type staticCommandStream struct {
	lines []string
	idx   int
}

func (s *staticCommandStream) Next(context.Context) (CommandOutput, error) {
	if s.idx >= len(s.lines) {
		return CommandOutput{}, io.EOF
	}
	line := s.lines[s.idx]
	s.idx++
	return CommandOutput{Stream: CommandStreamStdout, Text: line}, nil
}

func (s *staticCommandStream) Close() error { return nil }

type eventRunner struct {
	events   []schema.ExecEvent
	exitCode int
}

func (e eventRunner) Run(context.Context, RunRequest) (RunHandle, error) {
	return &eventHandle{events: e.events, exitCode: e.exitCode}, nil
}

func (eventRunner) RunCommand(context.Context, RunCommandRequest) (CommandHandle, error) {
	return nil, errors.New("command not supported")
}

type eventHandle struct {
	events   []schema.ExecEvent
	exitCode int
}

func (h *eventHandle) Events() EventStream { return &eventStream{events: h.events} }
func (h *eventHandle) Signal(context.Context, ProcessSignal) error {
	return nil
}
func (h *eventHandle) Wait(context.Context) (RunResult, error) {
	return RunResult{ExitCode: h.exitCode}, nil
}
func (h *eventHandle) Close() error { return nil }

type eventStream struct {
	events []schema.ExecEvent
	idx    int
}

func (s *eventStream) Next(context.Context) (schema.ExecEvent, error) {
	if s.idx >= len(s.events) {
		return schema.ExecEvent{}, io.EOF
	}
	event := s.events[s.idx]
	s.idx++
	return event, nil
}

func (s *eventStream) Close() error { return nil }
