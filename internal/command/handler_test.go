package command

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/internal/version"
	"pkt.systems/centaurx/schema"
)

func TestHandleNewOpensExistingRepo(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")

	var calls []schema.CreateTabRequest
	svc := &fakeService{
		createTabFn: func(_ context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error) {
			calls = append(calls, req)
			if len(calls) == 1 {
				if !req.CreateRepo {
					t.Fatalf("expected CreateRepo=true on first attempt")
				}
				return schema.CreateTabResponse{}, schema.ErrRepoExists
			}
			if req.CreateRepo {
				t.Fatalf("expected CreateRepo=false on retry")
			}
			return schema.CreateTabResponse{
				Tab: schema.TabSnapshot{
					ID:   "newtab",
					Name: "demo",
					Repo: schema.RepoRef{Name: "demo"},
				},
			}, nil
		},
		activateTabFn: func(_ context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error) {
			if req.TabID != "newtab" {
				t.Fatalf("unexpected tab id: %s", req.TabID)
			}
			return schema.ActivateTabResponse{Tab: schema.TabSnapshot{ID: req.TabID}}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			if len(req.Lines) == 0 || !strings.Contains(strings.Join(req.Lines, " "), "repo opened") {
				t.Fatalf("expected repo opened output, got %+v", req.Lines)
			}
			return schema.AppendOutputResponse{}, nil
		},
	}

	handler := NewHandler(svc, nil, HandlerConfig{})
	handled, err := handler.Handle(context.Background(), user, tabID, "/new demo")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled command")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 CreateTab calls, got %d", len(calls))
	}
}

func TestHandleModelUsage(t *testing.T) {
	handler := NewHandler(&fakeService{}, nil, HandlerConfig{
		AllowedModels: []schema.ModelID{"gpt-5.2-codex"},
	})
	_, err := handler.Handle(context.Background(), "alice", "tab1", "/model")
	if err == nil || !strings.Contains(err.Error(), "usage: /model") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestHandleHelpAppendsOutput(t *testing.T) {
	var captured []string
	svc := &fakeService{
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			captured = append(captured, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{
		AllowedModels: []schema.ModelID{"gpt-5.2-codex"},
	})
	_, err := handler.Handle(context.Background(), "alice", "tab1", "/help")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(captured) == 0 || captured[0] != schema.WorkedForMarker+"Commands" {
		t.Fatalf("expected commands output, got %+v", captured)
	}
	if len(captured) < 2 || !strings.HasPrefix(captured[1], schema.HelpMarker) {
		t.Fatalf("expected help marker line, got %+v", captured)
	}
}

func TestHandleHelpWithoutTabUsesSystemOutput(t *testing.T) {
	var captured []string
	svc := &fakeService{
		appendSystemOutputFn: func(_ context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
			captured = append(captured, req.Lines...)
			return schema.AppendSystemOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{
		AllowedModels: []schema.ModelID{"gpt-5.2-codex"},
	})
	_, err := handler.Handle(context.Background(), "alice", "", "/help")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(captured) == 0 || captured[0] != schema.WorkedForMarker+"Commands" {
		t.Fatalf("expected commands output, got %+v", captured)
	}
	if len(captured) < 2 || !strings.HasPrefix(captured[1], schema.HelpMarker) {
		t.Fatalf("expected help marker line, got %+v", captured)
	}
}

func TestHandleVersionAppendsOutput(t *testing.T) {
	var captured []string
	svc := &fakeService{
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			captured = append(captured, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})
	_, err := handler.Handle(context.Background(), "alice", "tab1", "/version")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(captured) < 5 {
		t.Fatalf("expected version output, got %+v", captured)
	}
	expectedVersion := version.Module() + " " + version.Current()
	if captured[0] != schema.WorkedForMarker+"About" {
		t.Fatalf("expected about header, got %q", captured[0])
	}
	if captured[1] != schema.AboutVersionMarker+expectedVersion {
		t.Fatalf("expected version line, got %q", captured[1])
	}
	if !strings.HasPrefix(captured[2], schema.AboutCopyrightMarker) {
		t.Fatalf("expected copyright line, got %q", captured[2])
	}
	if !strings.HasPrefix(captured[3], schema.AboutLinkMarker) {
		t.Fatalf("expected link line, got %q", captured[3])
	}
	if captured[len(captured)-1] != "" {
		t.Fatalf("expected trailing blank line, got %q", captured[len(captured)-1])
	}
}

func TestHandleVersionWithoutTabUsesSystemOutput(t *testing.T) {
	var captured []string
	svc := &fakeService{
		appendSystemOutputFn: func(_ context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
			captured = append(captured, req.Lines...)
			return schema.AppendSystemOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})
	_, err := handler.Handle(context.Background(), "alice", "", "/version")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(captured) == 0 || captured[0] != schema.WorkedForMarker+"About" {
		t.Fatalf("expected about output, got %+v", captured)
	}
}

func TestShellCommandWithoutTabUsesHome(t *testing.T) {
	lines := []string{}
	svc := &fakeService{
		appendSystemOutputFn: func(_ context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendSystemOutputResponse{}, nil
		},
	}
	runner := &fakeRunner{}
	provider := fakeRunnerProvider{
		resp: core.RunnerResponse{
			Runner: runner,
			Info:   core.RunnerInfo{HomeDir: "/home/test"},
		},
	}
	handler := NewHandler(svc, provider, HandlerConfig{})

	handled, err := handler.Handle(context.Background(), "tester", "", "! echo hi")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled command")
	}
	if runner.lastCmd.WorkingDir != "/home/test" {
		t.Fatalf("expected working dir /home/test, got %q", runner.lastCmd.WorkingDir)
	}
	if len(lines) == 0 || lines[0] != "$ echo hi" {
		t.Fatalf("expected command output, got %+v", lines)
	}
}

func TestHandleListReposAppendsOutput(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	var lines []string
	svc := &fakeService{
		listReposFn: func(_ context.Context, req schema.ListReposRequest) (schema.ListReposResponse, error) {
			if req.UserID != user {
				t.Fatalf("unexpected user: %s", req.UserID)
			}
			return schema.ListReposResponse{
				Repos: []schema.RepoRef{{Name: "demo"}, {Name: "notes"}},
			}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})
	handled, err := handler.Handle(context.Background(), user, tabID, "/listrepos")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled command")
	}
	if len(lines) == 0 || lines[0] != schema.WorkedForMarker+"Repos" {
		t.Fatalf("expected repos header, got %v", lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "- demo") || !strings.Contains(joined, "- notes") {
		t.Fatalf("unexpected list output: %v", lines)
	}
}

func TestHandleUnknownSlashCommandReturnsError(t *testing.T) {
	handler := NewHandler(&fakeService{}, nil, HandlerConfig{})
	handled, err := handler.Handle(context.Background(), "alice", "tab1", "/wat")
	if !handled {
		t.Fatalf("expected handled command")
	}
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got %v", err)
	}
}

func TestHandleNonSlashInputNotHandled(t *testing.T) {
	handler := NewHandler(&fakeService{}, nil, HandlerConfig{})
	handled, err := handler.Handle(context.Background(), "alice", "tab1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected non-slash input to be unhandled")
	}
}

func TestHandleThemeSetsTheme(t *testing.T) {
	var applied schema.ThemeName
	var lines []string
	svc := &fakeService{
		setThemeFn: func(_ context.Context, req schema.SetThemeRequest) (schema.SetThemeResponse, error) {
			applied = req.Theme
			return schema.SetThemeResponse{Theme: req.Theme}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})
	_, err := handler.Handle(context.Background(), "alice", "tab1", "/theme outrun")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if applied != "outrun" {
		t.Fatalf("expected theme outrun, got %q", applied)
	}
	if len(lines) == 0 || !strings.Contains(strings.Join(lines, "\n"), "theme set to outrun") {
		t.Fatalf("expected theme confirmation output, got %v", lines)
	}
}

func TestHandleRenewResetsSession(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	var called bool
	var lines []string
	svc := &fakeService{
		renewSessionFn: func(_ context.Context, req schema.RenewSessionRequest) (schema.RenewSessionResponse, error) {
			called = true
			if req.UserID != user || req.TabID != tabID {
				t.Fatalf("unexpected renew request: %+v", req)
			}
			return schema.RenewSessionResponse{Tab: schema.TabSnapshot{ID: tabID}}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})
	if _, err := handler.Handle(context.Background(), user, tabID, "/renew"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !called {
		t.Fatalf("expected RenewSession to be called")
	}
	if len(lines) == 0 || !strings.Contains(strings.Join(lines, " "), "session renewed") {
		t.Fatalf("expected session renewed output, got %v", lines)
	}
}

func TestToggleFullCommandOutput(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	prefs := sessionprefs.New()
	ctx := sessionprefs.WithContext(context.Background(), prefs)
	var captured []string
	svc := &fakeService{
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			captured = append(captured, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{})

	if _, err := handler.Handle(ctx, user, tabID, "/togglefullcommandoutput"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !prefs.FullCommandOutput {
		t.Fatalf("expected full command output enabled")
	}
	if len(captured) == 0 || !strings.Contains(captured[len(captured)-1], "command output: full") {
		t.Fatalf("expected toggle output line, got %v", captured)
	}

	if _, err := handler.Handle(ctx, user, tabID, "/togglefullcommandoutput"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if prefs.FullCommandOutput {
		t.Fatalf("expected full command output disabled")
	}
	if len(captured) == 0 || !strings.Contains(captured[len(captured)-1], "command output: terse") {
		t.Fatalf("expected toggle output line, got %v", captured)
	}
}

func TestHandleStatusShowsUsage(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	now := time.Date(2025, time.January, 2, 13, 0, 0, 0, time.UTC)
	reset := now.Add(2*time.Hour + 30*time.Minute).Unix()
	resetWeek := now.Add(24*time.Hour + 15*time.Minute).Unix()

	tab := schema.TabSnapshot{
		ID:        tabID,
		Name:      "demo",
		Repo:      schema.RepoRef{Name: "demo", Path: "/host/repos/demo"},
		Model:     "gpt-5.2-codex",
		SessionID: "sess-1",
	}

	var lines []string
	svc := &fakeService{
		listTabsFn: func(_ context.Context, _ schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs:      []schema.TabSnapshot{tab},
				ActiveTab: tabID,
			}, nil
		},
		getTabUsageFn: func(_ context.Context, req schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error) {
			if req.UserID != user || req.TabID != tabID {
				t.Fatalf("unexpected usage request: %+v", req)
			}
			return schema.GetTabUsageResponse{Usage: &schema.TurnUsage{InputTokens: 1500, OutputTokens: 500}}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}

	runner := &fakeUsageRunner{
		info: core.UsageInfo{
			ChatGPT: true,
			Primary: &core.UsageWindow{UsedPercent: 77, ResetAt: reset},
			Secondary: &core.UsageWindow{
				UsedPercent: 33,
				ResetAt:     resetWeek,
			},
		},
	}
	provider := fakeRunnerProvider{
		resp: core.RunnerResponse{
			Runner: runner,
			Info:   core.RunnerInfo{RepoRoot: "/repos"},
		},
	}
	handler := NewHandler(svc, provider, HandlerConfig{RepoRoot: "/host/repos"})
	handler.now = func() time.Time { return now }

	_, err := handler.Handle(context.Background(), user, tabID, "/status")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(lines) == 0 || lines[0] != schema.WorkedForMarker+"Status" {
		t.Fatalf("expected status header, got %v", lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Model:") || !strings.Contains(joined, "gpt-5.2-codex") {
		t.Fatalf("expected model line, got %v", lines)
	}
	if !strings.Contains(joined, "Directory:") || !strings.Contains(joined, "/repos/demo") {
		t.Fatalf("expected directory line, got %v", lines)
	}
	if !strings.Contains(joined, "Tokens used:") || !strings.Contains(joined, "2K") {
		t.Fatalf("expected tokens used line, got %v", lines)
	}
	if !strings.Contains(joined, "5h limit:") || !strings.Contains(joined, "23%") {
		t.Fatalf("expected 5h limit line, got %v", lines)
	}
	if !strings.Contains(joined, "Week limit:") || !strings.Contains(joined, "67%") {
		t.Fatalf("expected week limit line, got %v", lines)
	}
}

func TestHandleNewEmitsStatusWithoutTab(t *testing.T) {
	var systemLines []string
	service := &fakeService{
		createTabFn: func(_ context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error) {
			return schema.CreateTabResponse{
				Tab: schema.TabSnapshot{
					ID:   "tab-1",
					Name: "demo",
					Repo: schema.RepoRef{Name: "demo", Path: "/repos/demo"},
				},
				RepoCreated: true,
			}, nil
		},
		activateTabFn: func(_ context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error) {
			return schema.ActivateTabResponse{}, nil
		},
		appendSystemOutputFn: func(_ context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
			systemLines = append(systemLines, req.Lines...)
			return schema.AppendSystemOutputResponse{}, nil
		},
		appendOutputFn: func(context.Context, schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(service, fakeRunnerProvider{}, HandlerConfig{})
	_, err := handler.Handle(context.Background(), "alice", "", "/new demo")
	if err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	found := false
	for _, line := range systemLines {
		if strings.Contains(line, "status:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected status line in system output, got %v", systemLines)
	}
}

func TestHandleShellUsesRunner(t *testing.T) {
	repoRoot := "/repos-host"
	tab := schema.TabSnapshot{
		ID:   "tab1",
		Repo: schema.RepoRef{Path: repoRoot + "/alice/demo"},
	}
	svc := &fakeService{
		listTabsFn: func(_ context.Context, _ schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs:      []schema.TabSnapshot{tab},
				ActiveTab: tab.ID,
			}, nil
		},
	}

	runner := &fakeRunner{}
	provider := fakeRunnerProvider{
		resp: core.RunnerResponse{
			Runner: runner,
			Info:   core.RunnerInfo{RepoRoot: "/repos"},
		},
	}
	handler := NewHandler(svc, provider, HandlerConfig{
		RepoRoot: repoRoot,
	})

	_, err := handler.Handle(context.Background(), "alice", tab.ID, "!git status")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if runner.lastCmd.WorkingDir != "/repos/alice/demo" {
		t.Fatalf("expected mapped working dir, got %q", runner.lastCmd.WorkingDir)
	}
	if runner.lastCmd.Command != "git status" {
		t.Fatalf("unexpected command: %q", runner.lastCmd.Command)
	}
}

func TestHandleShellAppendsOutput(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	var lines []string
	var mu sync.Mutex
	svc := &fakeService{
		listTabsFn: func(_ context.Context, _ schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs: []schema.TabSnapshot{{
					ID:   tabID,
					Name: "demo",
					Repo: schema.RepoRef{Path: "/repos/demo"},
				}},
				ActiveTab: tabID,
			}, nil
		},
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			mu.Lock()
			lines = append(lines, req.Lines...)
			mu.Unlock()
			return schema.AppendOutputResponse{}, nil
		},
	}
	outputs := []core.CommandOutput{
		{Stream: core.CommandStreamStdout, Text: "hello"},
		{Stream: core.CommandStreamStderr, Text: "oops"},
	}
	runner := &outputRunner{
		outputs: outputs,
		result:  core.RunResult{ExitCode: 2},
	}
	provider := fakeRunnerProvider{
		resp: core.RunnerResponse{
			Runner: runner,
			Info:   core.RunnerInfo{RepoRoot: "/repos"},
		},
	}
	handler := NewHandler(svc, provider, HandlerConfig{RepoRoot: "/repos"})

	handled, err := handler.Handle(context.Background(), user, tabID, "!echo hello")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled command")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		joined := strings.Join(lines, "\n")
		mu.Unlock()
		if strings.Contains(joined, "$ echo hello") &&
			strings.Contains(joined, "hello") &&
			strings.Contains(joined, schema.StderrMarker+"oops") &&
			strings.Contains(joined, "command finished") &&
			strings.Contains(joined, "exit 2") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("expected command output lines, got %v", lines)
}

func TestHandleCloseClosesCurrentTab(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	closed := false
	svc := &fakeService{
		closeTabFn: func(_ context.Context, req schema.CloseTabRequest) (schema.CloseTabResponse, error) {
			if req.UserID != user || req.TabID != tabID {
				t.Fatalf("unexpected CloseTab request: %+v", req)
			}
			closed = true
			return schema.CloseTabResponse{}, nil
		},
		listTabsFn: func(_ context.Context, _ schema.ListTabsRequest) (schema.ListTabsResponse, error) {
			return schema.ListTabsResponse{
				Tabs:      []schema.TabSnapshot{{ID: tabID, Name: "demo"}},
				ActiveTab: tabID,
			}, nil
		},
		appendOutputFn: func(context.Context, schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			return schema.AppendOutputResponse{}, nil
		},
	}

	handler := NewHandler(svc, nil, HandlerConfig{})
	_, err := handler.Handle(context.Background(), user, tabID, "/close")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !closed {
		t.Fatalf("expected tab to be closed")
	}
}

func TestHandleLoginPubKeyCommands(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	var lines []string
	loginStore := &fakeLoginPubKeyStore{
		keys: []string{"ssh-ed25519 AAAAfirst", "ssh-ed25519 AAAAsecond"},
	}
	gitStore := &fakeGitKeyStore{pubKey: "ssh-ed25519 AAAAgit"}

	svc := &fakeService{
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}

	handler := NewHandler(svc, nil, HandlerConfig{
		LoginPubKeyStore: loginStore,
		GitKeyStore:      gitStore,
	})

	_, err := handler.Handle(context.Background(), user, tabID, "/addloginpubkey ssh-ed25519 AAAAnew")
	if err != nil {
		t.Fatalf("add login pubkey: %v", err)
	}
	if loginStore.addedKey == "" {
		t.Fatalf("expected login pubkey to be added")
	}

	lines = nil
	_, err = handler.Handle(context.Background(), user, tabID, "/listloginpubkeys")
	if err != nil {
		t.Fatalf("list login pubkeys: %v", err)
	}
	if len(lines) == 0 || lines[0] != schema.WorkedForMarker+"Login pubkeys" {
		t.Fatalf("expected list output, got %v", lines)
	}

	lines = nil
	_, err = handler.Handle(context.Background(), user, tabID, "/rmloginpubkey 2")
	if err != nil {
		t.Fatalf("remove login pubkey: %v", err)
	}
	if loginStore.removedIndex != 2 {
		t.Fatalf("expected remove index 2, got %d", loginStore.removedIndex)
	}

	lines = nil
	_, err = handler.Handle(context.Background(), user, tabID, "/pubkey")
	if err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	if len(lines) == 0 || lines[0] != schema.WorkedForMarker+"Git public key" {
		t.Fatalf("expected git pubkey header, got %v", lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "ssh-ed25519 AAAAgit") {
		t.Fatalf("expected git pubkey output, got %v", lines)
	}
}

func TestHandleRotateSSHKeyRequiresAffirm(t *testing.T) {
	handler := NewHandler(&fakeService{}, nil, HandlerConfig{})
	_, err := handler.Handle(context.Background(), "alice", "", "/rotatesshkey")
	if err == nil || !strings.Contains(err.Error(), "confirmation required") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
}

func TestHandleRotateSSHKeyAffirmRotates(t *testing.T) {
	user := schema.UserID("alice")
	tabID := schema.TabID("tab1")
	var lines []string
	rotator := &fakeGitKeyRotator{pubKey: "ssh-ed25519 AAAArotated"}
	svc := &fakeService{
		appendOutputFn: func(_ context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
			lines = append(lines, req.Lines...)
			return schema.AppendOutputResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, HandlerConfig{
		GitKeyRotator: rotator,
	})
	_, err := handler.Handle(context.Background(), user, tabID, "/rotatesshkey affirm")
	if err != nil {
		t.Fatalf("rotatesshkey: %v", err)
	}
	if !rotator.called || rotator.username != string(user) {
		t.Fatalf("expected rotator to be called for %s", user)
	}
	if len(lines) == 0 || !strings.Contains(strings.Join(lines, "\n"), "ssh key rotated") {
		t.Fatalf("expected rotation output, got %v", lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "ssh-ed25519 AAAArotated") {
		t.Fatalf("expected new pubkey output, got %v", lines)
	}
}

type fakeService struct {
	createTabFn          func(context.Context, schema.CreateTabRequest) (schema.CreateTabResponse, error)
	closeTabFn           func(context.Context, schema.CloseTabRequest) (schema.CloseTabResponse, error)
	activateTabFn        func(context.Context, schema.ActivateTabRequest) (schema.ActivateTabResponse, error)
	setModelFn           func(context.Context, schema.SetModelRequest) (schema.SetModelResponse, error)
	setThemeFn           func(context.Context, schema.SetThemeRequest) (schema.SetThemeResponse, error)
	appendOutputFn       func(context.Context, schema.AppendOutputRequest) (schema.AppendOutputResponse, error)
	appendSystemOutputFn func(context.Context, schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error)
	listTabsFn           func(context.Context, schema.ListTabsRequest) (schema.ListTabsResponse, error)
	listReposFn          func(context.Context, schema.ListReposRequest) (schema.ListReposResponse, error)
	getTabUsageFn        func(context.Context, schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error)
	renewSessionFn       func(context.Context, schema.RenewSessionRequest) (schema.RenewSessionResponse, error)
}

func (f *fakeService) CreateTab(ctx context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error) {
	if f.createTabFn != nil {
		return f.createTabFn(ctx, req)
	}
	return schema.CreateTabResponse{}, errors.New("unexpected CreateTab")
}

func (f *fakeService) CloseTab(ctx context.Context, req schema.CloseTabRequest) (schema.CloseTabResponse, error) {
	if f.closeTabFn != nil {
		return f.closeTabFn(ctx, req)
	}
	return schema.CloseTabResponse{}, errors.New("unexpected CloseTab")
}

func (f *fakeService) ListTabs(ctx context.Context, req schema.ListTabsRequest) (schema.ListTabsResponse, error) {
	if f.listTabsFn != nil {
		return f.listTabsFn(ctx, req)
	}
	return schema.ListTabsResponse{}, errors.New("unexpected ListTabs")
}

func (f *fakeService) ActivateTab(ctx context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error) {
	if f.activateTabFn != nil {
		return f.activateTabFn(ctx, req)
	}
	return schema.ActivateTabResponse{}, errors.New("unexpected ActivateTab")
}

func (f *fakeService) SendPrompt(context.Context, schema.SendPromptRequest) (schema.SendPromptResponse, error) {
	return schema.SendPromptResponse{}, errors.New("unexpected SendPrompt")
}

func (f *fakeService) SetModel(ctx context.Context, req schema.SetModelRequest) (schema.SetModelResponse, error) {
	if f.setModelFn != nil {
		return f.setModelFn(ctx, req)
	}
	return schema.SetModelResponse{}, errors.New("unexpected SetModel")
}

func (f *fakeService) SetTheme(ctx context.Context, req schema.SetThemeRequest) (schema.SetThemeResponse, error) {
	if f.setThemeFn != nil {
		return f.setThemeFn(ctx, req)
	}
	return schema.SetThemeResponse{}, errors.New("unexpected SetTheme")
}

func (f *fakeService) SwitchRepo(context.Context, schema.SwitchRepoRequest) (schema.SwitchRepoResponse, error) {
	return schema.SwitchRepoResponse{}, errors.New("unexpected SwitchRepo")
}

func (f *fakeService) SaveCodexAuth(context.Context, schema.SaveCodexAuthRequest) (schema.SaveCodexAuthResponse, error) {
	return schema.SaveCodexAuthResponse{}, errors.New("unexpected SaveCodexAuth")
}

func (f *fakeService) ListRepos(ctx context.Context, req schema.ListReposRequest) (schema.ListReposResponse, error) {
	if f.listReposFn != nil {
		return f.listReposFn(ctx, req)
	}
	return schema.ListReposResponse{}, errors.New("unexpected ListRepos")
}

func (f *fakeService) StopSession(context.Context, schema.StopSessionRequest) (schema.StopSessionResponse, error) {
	return schema.StopSessionResponse{}, errors.New("unexpected StopSession")
}

func (f *fakeService) RenewSession(ctx context.Context, req schema.RenewSessionRequest) (schema.RenewSessionResponse, error) {
	if f.renewSessionFn != nil {
		return f.renewSessionFn(ctx, req)
	}
	return schema.RenewSessionResponse{}, errors.New("unexpected RenewSession")
}

func (f *fakeService) GetBuffer(context.Context, schema.GetBufferRequest) (schema.GetBufferResponse, error) {
	return schema.GetBufferResponse{}, errors.New("unexpected GetBuffer")
}

func (f *fakeService) ScrollBuffer(context.Context, schema.ScrollBufferRequest) (schema.ScrollBufferResponse, error) {
	return schema.ScrollBufferResponse{}, errors.New("unexpected ScrollBuffer")
}

func (f *fakeService) AppendOutput(ctx context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
	if f.appendOutputFn != nil {
		return f.appendOutputFn(ctx, req)
	}
	return schema.AppendOutputResponse{}, errors.New("unexpected AppendOutput")
}

func (f *fakeService) AppendSystemOutput(ctx context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
	if f.appendSystemOutputFn != nil {
		return f.appendSystemOutputFn(ctx, req)
	}
	return schema.AppendSystemOutputResponse{}, errors.New("unexpected AppendSystemOutput")
}

func (f *fakeService) GetSystemBuffer(context.Context, schema.GetSystemBufferRequest) (schema.GetSystemBufferResponse, error) {
	return schema.GetSystemBufferResponse{}, errors.New("unexpected GetSystemBuffer")
}

func (f *fakeService) GetHistory(context.Context, schema.GetHistoryRequest) (schema.GetHistoryResponse, error) {
	return schema.GetHistoryResponse{}, errors.New("unexpected GetHistory")
}

func (f *fakeService) AppendHistory(context.Context, schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error) {
	return schema.AppendHistoryResponse{}, errors.New("unexpected AppendHistory")
}

func (f *fakeService) GetTabUsage(ctx context.Context, req schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error) {
	if f.getTabUsageFn != nil {
		return f.getTabUsageFn(ctx, req)
	}
	return schema.GetTabUsageResponse{}, errors.New("unexpected GetTabUsage")
}

type fakeRunner struct {
	lastCmd core.RunCommandRequest
}

func (f *fakeRunner) Run(context.Context, core.RunRequest) (core.RunHandle, error) {
	return nil, errors.New("unexpected Run")
}

func (f *fakeRunner) RunCommand(ctx context.Context, req core.RunCommandRequest) (core.CommandHandle, error) {
	f.lastCmd = req
	return &fakeCommandHandle{}, nil
}

type fakeCommandHandle struct{}

func (f *fakeCommandHandle) Outputs() core.CommandStream                      { return &fakeCommandStream{} }
func (f *fakeCommandHandle) Signal(context.Context, core.ProcessSignal) error { return nil }
func (f *fakeCommandHandle) Wait(context.Context) (core.RunResult, error) {
	return core.RunResult{ExitCode: 0}, nil
}
func (f *fakeCommandHandle) Close() error { return nil }

type fakeCommandStream struct{}

func (f *fakeCommandStream) Next(context.Context) (core.CommandOutput, error) {
	return core.CommandOutput{}, io.EOF
}
func (f *fakeCommandStream) Close() error { return nil }

type outputRunner struct {
	outputs []core.CommandOutput
	result  core.RunResult
}

func (r *outputRunner) Run(context.Context, core.RunRequest) (core.RunHandle, error) {
	return nil, errors.New("unexpected Run")
}

func (r *outputRunner) RunCommand(context.Context, core.RunCommandRequest) (core.CommandHandle, error) {
	return &outputCommandHandle{outputs: append([]core.CommandOutput(nil), r.outputs...), result: r.result}, nil
}

type outputCommandHandle struct {
	outputs []core.CommandOutput
	result  core.RunResult
}

func (h *outputCommandHandle) Outputs() core.CommandStream {
	return &outputCommandStream{outputs: h.outputs}
}
func (h *outputCommandHandle) Signal(context.Context, core.ProcessSignal) error { return nil }
func (h *outputCommandHandle) Wait(context.Context) (core.RunResult, error) {
	return h.result, nil
}
func (h *outputCommandHandle) Close() error { return nil }

type outputCommandStream struct {
	outputs []core.CommandOutput
	index   int
}

func (s *outputCommandStream) Next(context.Context) (core.CommandOutput, error) {
	if s.index >= len(s.outputs) {
		return core.CommandOutput{}, io.EOF
	}
	output := s.outputs[s.index]
	s.index++
	return output, nil
}
func (s *outputCommandStream) Close() error { return nil }

type fakeUsageRunner struct {
	info core.UsageInfo
	err  error
}

func (f *fakeUsageRunner) Run(context.Context, core.RunRequest) (core.RunHandle, error) {
	return nil, errors.New("unexpected Run")
}

func (f *fakeUsageRunner) RunCommand(context.Context, core.RunCommandRequest) (core.CommandHandle, error) {
	return nil, errors.New("unexpected RunCommand")
}

func (f *fakeUsageRunner) Usage(context.Context) (core.UsageInfo, error) {
	return f.info, f.err
}

type fakeRunnerProvider struct {
	resp core.RunnerResponse
}

func (f fakeRunnerProvider) RunnerFor(context.Context, core.RunnerRequest) (core.RunnerResponse, error) {
	return f.resp, nil
}

func (f fakeRunnerProvider) CloseTab(context.Context, core.RunnerCloseRequest) error {
	return nil
}

func (f fakeRunnerProvider) CloseAll(context.Context) error {
	return nil
}

type fakeLoginPubKeyStore struct {
	keys         []string
	addedKey     string
	removedIndex int
}

func (s *fakeLoginPubKeyStore) AddLoginPubKey(userID schema.UserID, key string) (int, error) {
	_ = userID
	s.addedKey = key
	s.keys = append(s.keys, key)
	return len(s.keys), nil
}

func (s *fakeLoginPubKeyStore) ListLoginPubKeys(userID schema.UserID) ([]string, error) {
	_ = userID
	return append([]string{}, s.keys...), nil
}

func (s *fakeLoginPubKeyStore) RemoveLoginPubKey(userID schema.UserID, index int) error {
	_ = userID
	s.removedIndex = index
	return nil
}

type fakeGitKeyStore struct {
	pubKey string
}

func (s *fakeGitKeyStore) LoadPublicKey(username string) (string, error) {
	_ = username
	return s.pubKey, nil
}

type fakeGitKeyRotator struct {
	called   bool
	username string
	keyType  string
	bits     int
	pubKey   string
	err      error
}

func (r *fakeGitKeyRotator) RotateKey(username, keyType string, bits int) (string, error) {
	r.called = true
	r.username = username
	r.keyType = keyType
	r.bits = bits
	return r.pubKey, r.err
}
