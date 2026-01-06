package sshserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pkt.systems/centaurx/schema"
)

func TestTerminalRefreshStateNoChangeKeepsClean(t *testing.T) {
	tabs := []schema.TabSnapshot{
		{
			ID:     "tab1",
			Name:   "demo",
			Status: schema.TabStatusIdle,
		},
	}
	buffer := schema.BufferSnapshot{
		TabID:    "tab1",
		Lines:    []string{"hello"},
		AtBottom: true,
	}

	svc := &stubService{
		listTabsFn: func(context.Context, schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs:      tabs,
				ActiveTab: "tab1",
			}, nil
		},
		getBufferFn: func(context.Context, schema.GetBufferRequest) (schema.GetBufferResponse, error) {
			return schema.GetBufferResponse{Buffer: buffer}, nil
		},
	}

	session := &terminalSession{
		service:   svc,
		userID:    "alice",
		tabStatus: make(map[schema.TabID]schema.TabStatus),
		queues:    make(map[schema.TabID][]string),
	}
	session.ctx = context.Background()
	session.SetSize(80, 24)

	session.refreshState()
	session.dirty = false

	session.refreshState()
	if session.dirty {
		t.Fatalf("expected refreshState to keep dirty=false when state unchanged")
	}
}

func TestRenderViewportAtBottomKeepsTail(t *testing.T) {
	theme := themeForName("outrun")
	longLine := schema.AgentMarker + strings.Repeat("a", 25)
	viewLines := []string{longLine, "LAST"}
	got := renderViewport(viewLines, 10, 3, theme, true)
	if len(got) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(got))
	}
	found := false
	for _, line := range got {
		if strings.Contains(sanitizeOutputLine(line), "LAST") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tail output to include LAST, got %q", got)
	}
}

func TestStylePromptPrefixSpinnerColored(t *testing.T) {
	theme := themeForName("outrun")
	prefix := string(spinnerFrames[0]) + " "
	styled := stylePromptPrefix(prefix, theme)
	if !strings.Contains(styled, ansiFgRGB(theme.SpinnerFG)) {
		t.Fatalf("expected spinner to be colored")
	}
}

func TestTerminalHistoryNavigationPreservesDraft(t *testing.T) {
	history := []string{"one", "two"}
	svc := &stubService{
		appendHistFn: func(_ context.Context, req schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error) {
			if strings.TrimSpace(req.Entry) != "" {
				if len(history) == 0 || history[len(history)-1] != req.Entry {
					history = append(history, req.Entry)
				}
			}
			return schema.AppendHistoryResponse{Entries: append([]string(nil), history...)}, nil
		},
	}
	session := &terminalSession{
		service:      svc,
		userID:       "alice",
		activeTab:    "tab1",
		history:      append([]string(nil), history...),
		historyIndex: -1,
	}
	session.editor.SetString("draft")
	session.historyUp()
	if got := session.editor.String(); got != "two" {
		t.Fatalf("expected history to move to 'two', got %q", got)
	}
	session.historyDown()
	if got := session.editor.String(); got != "draft" {
		t.Fatalf("expected history down to restore draft, got %q", got)
	}
}

func TestTerminalHistoryNavigationAtEndUsesHistory(t *testing.T) {
	session := &terminalSession{
		activeTab:    "tab1",
		history:      []string{"first", "second"},
		historyIndex: 1,
	}
	session.editor.SetString("line1\nline2")
	session.editor.cursor = session.editor.Len()
	session.handleKey(key{kind: keyUp})
	if got := session.editor.String(); got != "first" {
		t.Fatalf("expected history entry at end, got %q", got)
	}
}

func TestRenderInputLinesMultiline(t *testing.T) {
	lines, row, col := renderInputLines("> ", "first\nsecond", len([]rune("first\nsecond")), 20)
	if len(lines) != 2 {
		t.Fatalf("expected 2 input lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "> ") {
		t.Fatalf("expected first line to include prompt prefix, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatalf("expected second line to be indented, got %q", lines[1])
	}
	if row != 2 || col <= 0 {
		t.Fatalf("unexpected cursor position row=%d col=%d", row, col)
	}
}

func TestCommandSpinnerStopRequestsRedraw(t *testing.T) {
	session := &terminalSession{
		redrawCh: make(chan struct{}, 1),
	}
	stop := session.startCommandSpinner(1 * time.Millisecond)
	waitFor(t, 200*time.Millisecond, func() bool {
		return session.commandSpinner.Load()
	})
	drainChannel(session.redrawCh)
	stop()
	if session.commandSpinner.Load() {
		t.Fatalf("expected command spinner to stop")
	}
	select {
	case <-session.redrawCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected redraw signal after spinner stop")
	}
}

type blockingHandler struct {
	started chan struct{}
	done    chan struct{}
}

func (h blockingHandler) Handle(ctx context.Context, userID schema.UserID, tabID schema.TabID, input string) (bool, error) {
	if h.started != nil {
		close(h.started)
	}
	if h.done != nil {
		<-h.done
	}
	return true, nil
}

func TestCommandSpinnerStartsForNewCommand(t *testing.T) {
	previous := commandSpinnerDelay
	commandSpinnerDelay = 5 * time.Millisecond
	defer func() { commandSpinnerDelay = previous }()

	started := make(chan struct{})
	done := make(chan struct{})
	session := &terminalSession{
		handler:  blockingHandler{started: started, done: done},
		redrawCh: make(chan struct{}, 1),
		userID:   "alice",
		ctx:      context.Background(),
	}
	session.editor.SetString("/new demo")
	session.handleEnter()

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("handler did not start")
	}

	waitFor(t, 200*time.Millisecond, func() bool {
		return session.commandSpinner.Load()
	})

	close(done)
	waitFor(t, 200*time.Millisecond, func() bool {
		return !session.commandSpinner.Load() && session.commandActive.Load() == 0
	})
}

func TestCommandSpinnerStartsForBangCommand(t *testing.T) {
	previous := commandSpinnerDelay
	commandSpinnerDelay = 5 * time.Millisecond
	defer func() { commandSpinnerDelay = previous }()

	started := make(chan struct{})
	done := make(chan struct{})
	session := &terminalSession{
		handler:  blockingHandler{started: started, done: done},
		redrawCh: make(chan struct{}, 1),
		userID:   "alice",
		ctx:      context.Background(),
	}
	session.editor.SetString("!git status")

	enterDone := make(chan struct{})
	go func() {
		session.handleEnter()
		close(enterDone)
	}()

	select {
	case <-enterDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected ! command to run async")
	}

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("handler did not start")
	}

	waitFor(t, 200*time.Millisecond, func() bool {
		return session.commandSpinner.Load()
	})

	close(done)
	waitFor(t, 200*time.Millisecond, func() bool {
		return !session.commandSpinner.Load() && session.commandActive.Load() == 0
	})
}

type stubService struct {
	listTabsFn     func(context.Context, schema.ListTabsRequest) (schema.ListTabsResponse, error)
	getBufferFn    func(context.Context, schema.GetBufferRequest) (schema.GetBufferResponse, error)
	getSystemBufFn func(context.Context, schema.GetSystemBufferRequest) (schema.GetSystemBufferResponse, error)
	scrollBufferFn func(context.Context, schema.ScrollBufferRequest) (schema.ScrollBufferResponse, error)
	appendOutputFn func(context.Context, schema.AppendOutputRequest) (schema.AppendOutputResponse, error)
	getHistoryFn   func(context.Context, schema.GetHistoryRequest) (schema.GetHistoryResponse, error)
	appendHistFn   func(context.Context, schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error)
	createTabFn    func(context.Context, schema.CreateTabRequest) (schema.CreateTabResponse, error)
	closeTabFn     func(context.Context, schema.CloseTabRequest) (schema.CloseTabResponse, error)
	activateTabFn  func(context.Context, schema.ActivateTabRequest) (schema.ActivateTabResponse, error)
	sendPromptFn   func(context.Context, schema.SendPromptRequest) (schema.SendPromptResponse, error)
	setModelFn     func(context.Context, schema.SetModelRequest) (schema.SetModelResponse, error)
	setThemeFn     func(context.Context, schema.SetThemeRequest) (schema.SetThemeResponse, error)
	switchRepoFn   func(context.Context, schema.SwitchRepoRequest) (schema.SwitchRepoResponse, error)
	listReposFn    func(context.Context, schema.ListReposRequest) (schema.ListReposResponse, error)
	stopSessionFn  func(context.Context, schema.StopSessionRequest) (schema.StopSessionResponse, error)
	renewSessionFn func(context.Context, schema.RenewSessionRequest) (schema.RenewSessionResponse, error)
	appendSystemFn func(context.Context, schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error)
	getTabUsageFn  func(context.Context, schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error)
	saveCodexFn    func(context.Context, schema.SaveCodexAuthRequest) (schema.SaveCodexAuthResponse, error)
}

func waitFor(t *testing.T, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func drainChannel(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (s *stubService) CreateTab(ctx context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error) {
	if s.createTabFn != nil {
		return s.createTabFn(ctx, req)
	}
	return schema.CreateTabResponse{}, errors.New("unexpected CreateTab")
}

func (s *stubService) CloseTab(ctx context.Context, req schema.CloseTabRequest) (schema.CloseTabResponse, error) {
	if s.closeTabFn != nil {
		return s.closeTabFn(ctx, req)
	}
	return schema.CloseTabResponse{}, errors.New("unexpected CloseTab")
}

func (s *stubService) ListTabs(ctx context.Context, req schema.ListTabsRequest) (schema.ListTabsResponse, error) {
	if s.listTabsFn != nil {
		return s.listTabsFn(ctx, req)
	}
	return schema.ListTabsResponse{}, errors.New("unexpected ListTabs")
}

func (s *stubService) ActivateTab(ctx context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error) {
	if s.activateTabFn != nil {
		return s.activateTabFn(ctx, req)
	}
	return schema.ActivateTabResponse{}, errors.New("unexpected ActivateTab")
}

func (s *stubService) SendPrompt(ctx context.Context, req schema.SendPromptRequest) (schema.SendPromptResponse, error) {
	if s.sendPromptFn != nil {
		return s.sendPromptFn(ctx, req)
	}
	return schema.SendPromptResponse{}, errors.New("unexpected SendPrompt")
}

func (s *stubService) SetModel(ctx context.Context, req schema.SetModelRequest) (schema.SetModelResponse, error) {
	if s.setModelFn != nil {
		return s.setModelFn(ctx, req)
	}
	return schema.SetModelResponse{}, errors.New("unexpected SetModel")
}

func (s *stubService) SetTheme(ctx context.Context, req schema.SetThemeRequest) (schema.SetThemeResponse, error) {
	if s.setThemeFn != nil {
		return s.setThemeFn(ctx, req)
	}
	return schema.SetThemeResponse{}, errors.New("unexpected SetTheme")
}

func (s *stubService) SwitchRepo(ctx context.Context, req schema.SwitchRepoRequest) (schema.SwitchRepoResponse, error) {
	if s.switchRepoFn != nil {
		return s.switchRepoFn(ctx, req)
	}
	return schema.SwitchRepoResponse{}, errors.New("unexpected SwitchRepo")
}

func (s *stubService) ListRepos(ctx context.Context, req schema.ListReposRequest) (schema.ListReposResponse, error) {
	if s.listReposFn != nil {
		return s.listReposFn(ctx, req)
	}
	return schema.ListReposResponse{}, errors.New("unexpected ListRepos")
}

func (s *stubService) StopSession(ctx context.Context, req schema.StopSessionRequest) (schema.StopSessionResponse, error) {
	if s.stopSessionFn != nil {
		return s.stopSessionFn(ctx, req)
	}
	return schema.StopSessionResponse{}, errors.New("unexpected StopSession")
}

func (s *stubService) RenewSession(ctx context.Context, req schema.RenewSessionRequest) (schema.RenewSessionResponse, error) {
	if s.renewSessionFn != nil {
		return s.renewSessionFn(ctx, req)
	}
	return schema.RenewSessionResponse{}, errors.New("unexpected RenewSession")
}

func (s *stubService) GetBuffer(ctx context.Context, req schema.GetBufferRequest) (schema.GetBufferResponse, error) {
	if s.getBufferFn != nil {
		return s.getBufferFn(ctx, req)
	}
	return schema.GetBufferResponse{}, errors.New("unexpected GetBuffer")
}

func (s *stubService) ScrollBuffer(ctx context.Context, req schema.ScrollBufferRequest) (schema.ScrollBufferResponse, error) {
	if s.scrollBufferFn != nil {
		return s.scrollBufferFn(ctx, req)
	}
	return schema.ScrollBufferResponse{}, errors.New("unexpected ScrollBuffer")
}

func (s *stubService) AppendOutput(ctx context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
	if s.appendOutputFn != nil {
		return s.appendOutputFn(ctx, req)
	}
	return schema.AppendOutputResponse{}, errors.New("unexpected AppendOutput")
}

func (s *stubService) AppendSystemOutput(ctx context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
	if s.appendSystemFn != nil {
		return s.appendSystemFn(ctx, req)
	}
	return schema.AppendSystemOutputResponse{}, errors.New("unexpected AppendSystemOutput")
}

func (s *stubService) GetSystemBuffer(ctx context.Context, req schema.GetSystemBufferRequest) (schema.GetSystemBufferResponse, error) {
	if s.getSystemBufFn != nil {
		return s.getSystemBufFn(ctx, req)
	}
	return schema.GetSystemBufferResponse{}, errors.New("unexpected GetSystemBuffer")
}

func (s *stubService) GetHistory(ctx context.Context, req schema.GetHistoryRequest) (schema.GetHistoryResponse, error) {
	if s.getHistoryFn != nil {
		return s.getHistoryFn(ctx, req)
	}
	return schema.GetHistoryResponse{}, errors.New("unexpected GetHistory")
}

func (s *stubService) AppendHistory(ctx context.Context, req schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error) {
	if s.appendHistFn != nil {
		return s.appendHistFn(ctx, req)
	}
	return schema.AppendHistoryResponse{}, errors.New("unexpected AppendHistory")
}

func (s *stubService) SaveCodexAuth(ctx context.Context, req schema.SaveCodexAuthRequest) (schema.SaveCodexAuthResponse, error) {
	if s.saveCodexFn != nil {
		return s.saveCodexFn(ctx, req)
	}
	return schema.SaveCodexAuthResponse{}, errors.New("unexpected SaveCodexAuth")
}

func (s *stubService) GetTabUsage(ctx context.Context, req schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error) {
	if s.getTabUsageFn != nil {
		return s.getTabUsageFn(ctx, req)
	}
	return schema.GetTabUsageResponse{}, errors.New("unexpected GetTabUsage")
}
