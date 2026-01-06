package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pkt.systems/centaurx/internal/format"
	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/internal/persist"
	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// service implements the core service behavior.
type service struct {
	cfg      schema.ServiceConfig
	repoRoot string
	runners  RunnerProvider
	renderer Renderer
	sink     EventSink
	store    *persist.Store
	repos    RepoResolver
	logger   pslog.Logger
	mu       sync.Mutex
	userTabs map[schema.UserID]*userState
}

var stopSleep = time.Sleep

type userState struct {
	tabs   map[schema.TabID]*tab
	order  []schema.TabID
	system *buffer
	theme  schema.ThemeName
}

// NewService constructs the core service implementation.
func NewService(cfg schema.ServiceConfig, deps ServiceDeps) (Service, error) {
	normalized, err := schema.NormalizeServiceConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = normalized
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		return nil, err
	}
	if deps.Renderer == nil {
		deps.Renderer = format.NewPlainRenderer()
	}
	if deps.RepoResolver == nil {
		resolver, err := NewRepoResolver(cfg.RepoRoot)
		if err != nil {
			return nil, err
		}
		deps.RepoResolver = resolver
	}
	var store *persist.Store
	if cfg.StateDir != "" {
		store, err = persist.NewStoreWithLogger(cfg.StateDir, deps.Logger)
		if err != nil {
			return nil, err
		}
	}
	logger := deps.Logger
	if logger == nil {
		logger = pslog.Ctx(context.Background())
	}
	return &service{
		cfg:      cfg,
		repoRoot: cfg.RepoRoot,
		runners:  deps.RunnerProvider,
		renderer: deps.Renderer,
		sink:     deps.EventSink,
		store:    store,
		repos:    deps.RepoResolver,
		logger:   logger,
		userTabs: make(map[schema.UserID]*userState),
	}, nil
}

func (s *service) CreateTab(ctx context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error) {
	if ctx == nil {
		return schema.CreateTabResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.CreateTabResponse{}, err
	}
	log := logx.WithUser(ctx, userID)
	log.Info("service tab create start", "repo_name", req.RepoName, "repo_url", req.RepoURL, "create_repo", req.CreateRepo, "tab_name", req.TabName)
	if strings.TrimSpace(req.RepoURL) == "" && strings.TrimSpace(string(req.RepoName)) == "" {
		return schema.CreateTabResponse{}, schema.ErrInvalidRepo
	}
	if strings.TrimSpace(string(req.RepoName)) == "" {
		if strings.TrimSpace(req.RepoURL) == "" {
			return schema.CreateTabResponse{}, schema.ErrInvalidRepo
		}
	}

	tabID := schema.TabID(newID())

	var repoRef schema.RepoRef
	repoCreated := false
	if strings.TrimSpace(req.RepoURL) != "" {
		repoResp, err := s.repos.OpenOrCloneURL(ctx, OpenOrCloneRequest{UserID: userID, TabID: tabID, URL: req.RepoURL})
		if err != nil {
			log.Warn("service tab create failed", "err", err)
			if s.runners != nil {
				_ = s.runners.CloseTab(ctx, RunnerCloseRequest{UserID: userID, TabID: tabID})
			}
			return schema.CreateTabResponse{}, err
		}
		repoRef = repoResp.Repo
		repoCreated = repoResp.Created
	} else if req.CreateRepo {
		repoResp, err := s.repos.CreateRepo(ctx, CreateRepoRequest{UserID: userID, TabID: tabID, Name: req.RepoName})
		if err != nil {
			log.Warn("service tab create failed", "err", err)
			if s.runners != nil {
				_ = s.runners.CloseTab(ctx, RunnerCloseRequest{UserID: userID, TabID: tabID})
			}
			return schema.CreateTabResponse{}, err
		}
		repoRef = repoResp.Repo
		repoCreated = true
	} else {
		repoResp, err := s.repos.ResolveRepo(ctx, ResolveRepoRequest{UserID: userID, Name: req.RepoName})
		if err != nil {
			log.Warn("service tab create failed", "err", err)
			if s.runners != nil {
				_ = s.runners.CloseTab(ctx, RunnerCloseRequest{UserID: userID, TabID: tabID})
			}
			return schema.CreateTabResponse{}, err
		}
		repoRef = repoResp.Repo
	}
	tabName := req.TabName
	if strings.TrimSpace(string(tabName)) == "" {
		tabName = schema.TabName(repoRef.Name)
	}
	tabName = schema.TabName(formatTabName(string(tabName), s.cfg.TabNameMax, s.cfg.TabNameSuffix))

	tab := &tab{
		ID:      tabID,
		Name:    tabName,
		Repo:    repoRef,
		Model:   s.cfg.DefaultModel,
		Status:  schema.TabStatusIdle,
		buffer:  newBufferWithMaxLines(s.cfg.BufferMaxLines),
		history: newHistory(defaultHistoryMax),
	}

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	state.tabs[tab.ID] = tab
	state.order = append(state.order, tab.ID)
	active := activeTabFromContext(ctx, state)
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventCreated,
		Tab:       tab.Snapshot(active == tab.ID),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	s.persistUser(log, userID)
	logx.WithRepo(log.With("tab", tab.ID, "tab_name", tab.Name, "repo_created", repoCreated), repoRef).Info("service tab created")

	return schema.CreateTabResponse{Tab: tab.Snapshot(active == tab.ID), RepoCreated: repoCreated}, nil
}

func (s *service) CloseTab(ctx context.Context, req schema.CloseTabRequest) (schema.CloseTabResponse, error) {
	if ctx == nil {
		return schema.CloseTabResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.CloseTabResponse{}, err
	}
	baseLog := logx.WithUserTab(ctx, userID, req.TabID)
	ctx = logx.ContextWithUserTabLogger(ctx, baseLog, userID, req.TabID)
	log := baseLog

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	handle := RunHandle(nil)
	runCancel := context.CancelFunc(nil)
	var commands []commandRun
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service tab close failed", "err", schema.ErrTabNotFound)
		return schema.CloseTabResponse{}, schema.ErrTabNotFound
	}
	handle = tab.Run
	runCancel = tab.RunCancel
	if len(tab.commands) > 0 {
		commands = append([]commandRun(nil), tab.commands...)
	}
	delete(state.tabs, req.TabID)
	state.order = removeTabID(state.order, req.TabID)
	if prefs := sessionprefs.FromContext(ctx); prefs != nil && prefs.ActiveTab == req.TabID {
		prefs.ActiveTab = ""
	}
	active := activeTabFromContext(ctx, state)
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventClosed,
		Tab:       tab.Snapshot(false),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	s.persistUser(log, userID)
	if s.runners != nil {
		_ = s.runners.CloseTab(ctx, RunnerCloseRequest{UserID: userID, TabID: req.TabID})
	}
	if handle != nil || len(commands) > 0 {
		go s.stopTabHandles(log, userID, req.TabID, handle, runCancel, commands)
	}
	log.Info("service tab closed")
	return schema.CloseTabResponse{Tab: tab.Snapshot(false)}, nil
}

func (s *service) ListTabs(ctx context.Context, req schema.ListTabsRequest) (schema.ListTabsResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.ListTabsResponse{}, err
	}
	log := logx.WithUser(ctx, userID)

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateUserStateLocked(userID)
	active := activeTabFromContext(ctx, state)
	tabs := make([]schema.TabSnapshot, 0, len(state.order))
	for _, id := range state.order {
		tab := state.tabs[id]
		if tab == nil {
			continue
		}
		tabs = append(tabs, tab.Snapshot(id == active))
	}

	var activeRepo schema.RepoRef
	if active != "" {
		if tab := state.tabs[active]; tab != nil {
			activeRepo = tab.Repo
		}
	}

	resp := schema.ListTabsResponse{
		Tabs:       tabs,
		ActiveTab:  active,
		ActiveRepo: activeRepo,
		Theme:      state.theme,
	}
	log.Trace("service tabs listed", "count", len(tabs), "active", resp.ActiveTab)
	return resp, nil
}

func (s *service) ActivateTab(ctx context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error) {
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.ActivateTabResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service tab activate failed", "err", schema.ErrTabNotFound)
		return schema.ActivateTabResponse{}, schema.ErrTabNotFound
	}
	if prefs := sessionprefs.FromContext(ctx); prefs != nil {
		prefs.ActiveTab = req.TabID
	}
	active := activeTabFromContext(ctx, state)
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventActivated,
		Tab:       tab.Snapshot(active == tab.ID),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	s.persistUser(log, userID)
	log.Info("service tab activated")
	return schema.ActivateTabResponse{Tab: tab.Snapshot(active == tab.ID)}, nil
}

func (s *service) SendPrompt(ctx context.Context, req schema.SendPromptRequest) (schema.SendPromptResponse, error) {
	if s.runners == nil {
		return schema.SendPromptResponse{}, schema.ErrRunnerUnavailable
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return schema.SendPromptResponse{}, schema.ErrEmptyPrompt
	}
	if ctx == nil {
		return schema.SendPromptResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.SendPromptResponse{}, err
	}
	baseLog := logx.WithUserTab(ctx, userID, req.TabID)
	ctx = logx.ContextWithUserTabLogger(ctx, baseLog, userID, req.TabID)
	log := baseLog

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	active := activeTabFromContext(ctx, state)
	if tab != nil && tab.Status == schema.TabStatusRunning {
		s.mu.Unlock()
		log.Warn("service prompt rejected", "err", schema.ErrTabBusy)
		return schema.SendPromptResponse{}, schema.ErrTabBusy
	}
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service prompt rejected", "err", schema.ErrTabNotFound)
		return schema.SendPromptResponse{}, schema.ErrTabNotFound
	}
	sessionLog := logx.WithSession(baseLog, tab.SessionID)
	ctx = logx.ContextWithUserTabLogger(ctx, sessionLog, userID, req.TabID)
	log = logx.WithRepo(sessionLog, tab.Repo).With("model", tab.Model, "prompt_len", len(req.Prompt))
	log.Info("service prompt start")
	s.appendLine(log, userID, tab.ID, fmt.Sprintf("> %s", req.Prompt))

	runCtx, runCancel := detachRunContext(ctx)
	runnerResp, err := s.runners.RunnerFor(runCtx, RunnerRequest{UserID: userID, TabID: tab.ID})
	if err != nil {
		log.Error("service runner lookup failed", "err", err)
		startLines := buildExecStartLines(time.Now(), tab, gitSummary{})
		s.appendLines(log, userID, tab.ID, startLines)
		s.appendErrorLine(log, userID, tab.ID, err)
		if runCancel != nil {
			runCancel()
		}
		return schema.SendPromptResponse{}, err
	}
	runner := runnerResp.Runner
	info := runnerResp.Info
	workingDir := tab.Repo.Path
	if info.RepoRoot != "" {
		mapped, err := MapRepoPath(s.repoRoot, info.RepoRoot, tab.Repo.Path)
		if err != nil {
			log.Error("service repo map failed", "err", err)
			s.appendErrorLine(log, userID, tab.ID, err)
			if runCancel != nil {
				runCancel()
			}
			return schema.SendPromptResponse{}, err
		}
		workingDir = mapped
	}
	if !s.cfg.DisableAuditLogging {
		command := "codex exec --json"
		if tab.SessionID != "" {
			command = fmt.Sprintf("codex exec resume %s --json", tab.SessionID)
		}
		auditLog := logx.WithRepo(sessionLog, tab.Repo).With("model", tab.Model)
		auditLog.Debug("audit command", "command_type", "codex", "command", command, "workdir", workingDir)
	}
	startLines := buildExecStartLines(time.Now(), tab, collectGitSummary(runCtx, runner, workingDir, info.SSHAuthSock))
	s.appendLines(log, userID, tab.ID, startLines)
	runReq := RunRequest{
		WorkingDir:      workingDir,
		Prompt:          req.Prompt,
		Model:           tab.Model,
		ResumeSessionID: tab.SessionID,
		JSON:            true,
		SSHAuthSock:     info.SSHAuthSock,
	}
	started := time.Now()
	handle, err := runner.Run(runCtx, runReq)
	if err != nil {
		log.Error("service runner start failed", "err", err)
		s.appendErrorLine(log, userID, tab.ID, err)
		if runCancel != nil {
			runCancel()
		}
		return schema.SendPromptResponse{}, err
	}

	s.mu.Lock()
	tab.Status = schema.TabStatusRunning
	tab.Run = handle
	tab.RunCancel = runCancel
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventStatus,
		Tab:       tab.Snapshot(tab.ID == active),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	log.Info("service runner started", "workdir", workingDir)

	go s.consumeEvents(runCtx, userID, tab.ID, handle, runCancel, started)
	return schema.SendPromptResponse{Tab: tab.Snapshot(tab.ID == active), Accepted: true}, nil
}

func (s *service) SetModel(ctx context.Context, req schema.SetModelRequest) (schema.SetModelResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.SetModelResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	normalizedModel, err := schema.NormalizeModelID(string(req.Model))
	if err != nil {
		return schema.SetModelResponse{}, err
	}

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service model update failed", "err", schema.ErrTabNotFound)
		return schema.SetModelResponse{}, schema.ErrTabNotFound
	}
	tab.Model = normalizedModel
	active := activeTabFromContext(ctx, state)
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventUpdated,
		Tab:       tab.Snapshot(req.TabID == active),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	s.persistUser(log, userID)
	log.Info("service model updated", "model", normalizedModel)
	return schema.SetModelResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
}

func (s *service) SwitchRepo(ctx context.Context, req schema.SwitchRepoRequest) (schema.SwitchRepoResponse, error) {
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.SwitchRepoResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	repoResp, err := s.repos.ResolveRepo(ctx, ResolveRepoRequest{UserID: userID, Name: req.RepoName})
	if err != nil {
		log.Warn("service repo switch failed", "err", err, "repo_name", req.RepoName)
		return schema.SwitchRepoResponse{}, err
	}
	repoRef := repoResp.Repo

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service repo switch failed", "err", schema.ErrTabNotFound)
		return schema.SwitchRepoResponse{}, schema.ErrTabNotFound
	}
	tab.Repo = repoRef
	active := activeTabFromContext(ctx, state)
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventUpdated,
		Tab:       tab.Snapshot(req.TabID == active),
		ActiveTab: active,
	}
	s.mu.Unlock()
	s.emitTabEvent(event)
	s.persistUser(log, userID)
	logx.WithRepo(log, repoRef).Info("service repo switched")
	return schema.SwitchRepoResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
}

func (s *service) ListRepos(ctx context.Context, req schema.ListReposRequest) (schema.ListReposResponse, error) {
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.ListReposResponse{}, err
	}
	log := logx.WithUser(ctx, userID)
	listResp, err := s.repos.ListRepos(ctx, ListReposRequest{UserID: userID})
	if err != nil {
		log.Warn("service repos list failed", "err", err)
		return schema.ListReposResponse{}, err
	}
	log.Debug("service repos listed", "count", len(listResp.Repos))
	return schema.ListReposResponse{Repos: listResp.Repos}, nil
}

func (s *service) StopSession(ctx context.Context, req schema.StopSessionRequest) (schema.StopSessionResponse, error) {
	if s.runners == nil {
		return schema.StopSessionResponse{}, schema.ErrRunnerUnavailable
	}
	if ctx == nil {
		return schema.StopSessionResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.StopSessionResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	active := activeTabFromContext(ctx, state)
	handle := RunHandle(nil)
	var commands []commandRun
	if tab != nil {
		handle = tab.Run
		if len(tab.commands) > 0 {
			commands = append([]commandRun(nil), tab.commands...)
		}
	}
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service stop failed", "err", schema.ErrTabNotFound)
		return schema.StopSessionResponse{}, schema.ErrTabNotFound
	}

	if handle == nil && len(commands) == 0 {
		log.Info("service stop ignored", "reason", "no running process")
		s.appendLine(log, userID, req.TabID, "stop requested: no running process")
		return schema.StopSessionResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
	}

	log.Info("service stop requested")
	s.appendLine(log, userID, req.TabID, "stop requested: sending SIGTERM")
	go s.stopTabHandlesAsync(log, userID, req.TabID, handle, tab.RunCancel, commands)

	return schema.StopSessionResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
}

func (s *service) stopTabHandles(log pslog.Logger, userID schema.UserID, tabID schema.TabID, handle RunHandle, runCancel context.CancelFunc, commands []commandRun) {
	signalCtx := context.Background()
	if log != nil {
		signalCtx = logx.ContextWithUserTabLogger(signalCtx, log, userID, tabID)
	}
	if handle != nil {
		if err := handle.Signal(signalCtx, ProcessSignalTERM); err != nil && log != nil {
			log.Warn("service stop signal failed", "signal", ProcessSignalTERM, "err", err)
		}
	}
	for _, cmd := range commands {
		if cmd.handle == nil {
			continue
		}
		if err := cmd.handle.Signal(signalCtx, ProcessSignalTERM); err != nil && log != nil {
			log.Warn("service stop signal failed", "signal", ProcessSignalTERM, "err", err)
		}
	}
	stopSleep(10 * time.Second)
	shouldKill := handle != nil && !isDone(handleDone(handle))
	if !shouldKill {
		for _, cmd := range commands {
			if cmd.handle == nil {
				continue
			}
			if !isDone(handleDone(cmd.handle)) {
				shouldKill = true
				break
			}
		}
	}
	if handle != nil && shouldKill && !isDone(handleDone(handle)) {
		if err := handle.Signal(signalCtx, ProcessSignalKILL); err != nil && log != nil {
			log.Warn("service stop signal failed", "signal", ProcessSignalKILL, "err", err)
		}
	}
	for _, cmd := range commands {
		if cmd.handle == nil {
			continue
		}
		if shouldKill && !isDone(handleDone(cmd.handle)) {
			if err := cmd.handle.Signal(signalCtx, ProcessSignalKILL); err != nil && log != nil {
				log.Warn("service stop signal failed", "signal", ProcessSignalKILL, "err", err)
			}
		}
	}
	if runCancel != nil {
		runCancel()
	}
	for _, cmd := range commands {
		if cmd.cancel != nil {
			cmd.cancel()
		}
	}
	if log != nil {
		log.Info("stop requested: signal sequence complete")
	}
}

func (s *service) stopTabHandlesAsync(log pslog.Logger, userID schema.UserID, tabID schema.TabID, handle RunHandle, runCancel context.CancelFunc, commands []commandRun) {
	signalCtx := context.Background()
	if log != nil {
		signalCtx = logx.ContextWithUserTabLogger(signalCtx, log, userID, tabID)
	}
	if handle != nil {
		if err := handle.Signal(signalCtx, ProcessSignalTERM); err != nil {
			if log != nil {
				log.Warn("service stop signal failed", "signal", ProcessSignalTERM, "err", err)
			}
			s.appendLine(log, userID, tabID, fmt.Sprintf("signal error: %v", err))
		}
	}
	for _, cmd := range commands {
		if cmd.handle == nil {
			continue
		}
		if err := cmd.handle.Signal(signalCtx, ProcessSignalTERM); err != nil {
			if log != nil {
				log.Warn("service stop signal failed", "signal", ProcessSignalTERM, "err", err)
			}
			s.appendLine(log, userID, tabID, fmt.Sprintf("signal error: %v", err))
		}
	}
	stopSleep(10 * time.Second)
	shouldKill := handle != nil && !isDone(handleDone(handle))
	if !shouldKill {
		for _, cmd := range commands {
			if cmd.handle == nil {
				continue
			}
			if !isDone(handleDone(cmd.handle)) {
				shouldKill = true
				break
			}
		}
	}
	if shouldKill {
		s.appendLine(log, userID, tabID, "stop requested: sending SIGKILL")
	}
	if handle != nil {
		if shouldKill && !isDone(handleDone(handle)) {
			if err := handle.Signal(signalCtx, ProcessSignalKILL); err != nil {
				if log != nil {
					log.Warn("service stop signal failed", "signal", ProcessSignalKILL, "err", err)
				}
				s.appendLine(log, userID, tabID, fmt.Sprintf("signal error: %v", err))
			}
		}
	}
	for _, cmd := range commands {
		if cmd.handle == nil {
			continue
		}
		if shouldKill && !isDone(handleDone(cmd.handle)) {
			if err := cmd.handle.Signal(signalCtx, ProcessSignalKILL); err != nil {
				if log != nil {
					log.Warn("service stop signal failed", "signal", ProcessSignalKILL, "err", err)
				}
				s.appendLine(log, userID, tabID, fmt.Sprintf("signal error: %v", err))
			}
		}
	}
	if log != nil {
		log.Info("stop requested: signal sequence complete")
		log.Info("service stop completed")
	}
	if runCancel != nil {
		runCancel()
	}
	for _, cmd := range commands {
		if cmd.cancel != nil {
			cmd.cancel()
		}
	}
}

func handleDone(h any) <-chan struct{} {
	if h == nil {
		return nil
	}
	if done, ok := h.(interface{ Done() <-chan struct{} }); ok {
		return done.Done()
	}
	return nil
}

func isDone(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (s *service) RenewSession(ctx context.Context, req schema.RenewSessionRequest) (schema.RenewSessionResponse, error) {
	if ctx == nil {
		return schema.RenewSessionResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.RenewSessionResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)

	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	active := activeTabFromContext(ctx, state)
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service renew failed", "err", schema.ErrTabNotFound)
		return schema.RenewSessionResponse{}, schema.ErrTabNotFound
	}
	if tab.Status == schema.TabStatusRunning {
		s.mu.Unlock()
		log.Warn("service renew failed", "err", schema.ErrTabBusy)
		return schema.RenewSessionResponse{}, schema.ErrTabBusy
	}
	tab.SessionID = ""
	tab.LastUsage = nil
	event := schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventUpdated,
		Tab:       tab.Snapshot(req.TabID == active),
		ActiveTab: active,
	}
	s.mu.Unlock()

	s.emitTabEvent(event)
	s.persistUser(log, userID)
	log.Info("service session renewed")
	return schema.RenewSessionResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
}

func (s *service) SetTheme(ctx context.Context, req schema.SetThemeRequest) (schema.SetThemeResponse, error) {
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.SetThemeResponse{}, err
	}
	log := logx.WithUser(ctx, userID)
	if strings.TrimSpace(string(req.Theme)) == "" {
		return schema.SetThemeResponse{}, errors.New("theme is required")
	}

	var tabSnapshot schema.TabSnapshot
	var active schema.TabID
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	state.theme = req.Theme
	active = activeTabFromContext(ctx, state)
	if active != "" {
		if tab := state.tabs[active]; tab != nil {
			tabSnapshot = tab.Snapshot(true)
		}
	}
	s.mu.Unlock()
	s.emitTabEvent(schema.TabEvent{
		UserID:    userID,
		Type:      schema.TabEventUpdated,
		Tab:       tabSnapshot,
		ActiveTab: active,
		Theme:     req.Theme,
	})
	s.persistUser(log, userID)
	log.Info("service theme updated", "theme", req.Theme)
	return schema.SetThemeResponse{Theme: req.Theme}, nil
}

func (s *service) GetBuffer(ctx context.Context, req schema.GetBufferRequest) (schema.GetBufferResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.GetBufferResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)

	s.mu.Lock()
	tab := s.getOrCreateUserStateLocked(userID).tabs[req.TabID]
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service buffer get failed", "err", schema.ErrTabNotFound)
		return schema.GetBufferResponse{}, schema.ErrTabNotFound
	}

	view := tab.buffer.Snapshot(req.Limit)
	log.Trace("service buffer snapshot", "lines", view.TotalLines, "offset", view.ScrollOffset, "limit", req.Limit)
	return schema.GetBufferResponse{Buffer: mapBufferSnapshot(req.TabID, view)}, nil
}

func (s *service) ScrollBuffer(ctx context.Context, req schema.ScrollBufferRequest) (schema.ScrollBufferResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.ScrollBufferResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)

	s.mu.Lock()
	tab := s.getOrCreateUserStateLocked(userID).tabs[req.TabID]
	if tab != nil {
		tab.buffer.Scroll(req.Delta, req.Limit)
	}
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service buffer scroll failed", "err", schema.ErrTabNotFound)
		return schema.ScrollBufferResponse{}, schema.ErrTabNotFound
	}

	view := tab.buffer.Snapshot(req.Limit)
	s.persistUser(log, userID)
	log.Debug("service buffer scrolled", "offset", view.ScrollOffset, "limit", req.Limit)
	return schema.ScrollBufferResponse{Buffer: mapBufferSnapshot(req.TabID, view)}, nil
}

func (s *service) AppendOutput(ctx context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error) {
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.AppendOutputResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	if len(req.Lines) == 0 {
		return schema.AppendOutputResponse{}, nil
	}
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	active := activeTabFromContext(ctx, state)
	if tab != nil && tab.buffer != nil {
		tab.buffer.Append(req.Lines...)
	}
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service output append failed", "err", schema.ErrTabNotFound)
		return schema.AppendOutputResponse{}, schema.ErrTabNotFound
	}
	s.emitOutput(userID, req.TabID, req.Lines)
	s.persistUser(log, userID)
	log.Trace("service output appended", "lines", len(req.Lines))
	return schema.AppendOutputResponse{Tab: tab.Snapshot(req.TabID == active)}, nil
}

func (s *service) AppendSystemOutput(ctx context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.AppendSystemOutputResponse{}, err
	}
	log := logx.WithUser(ctx, userID)
	if len(req.Lines) == 0 {
		return schema.AppendSystemOutputResponse{}, nil
	}
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	if state.system != nil {
		state.system.Append(req.Lines...)
	}
	s.mu.Unlock()
	s.emitSystemOutput(userID, req.Lines)
	s.persistUser(log, userID)
	log.Trace("service system output appended", "lines", len(req.Lines))
	return schema.AppendSystemOutputResponse{}, nil
}

func (s *service) GetSystemBuffer(ctx context.Context, req schema.GetSystemBufferRequest) (schema.GetSystemBufferResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.GetSystemBufferResponse{}, err
	}
	log := logx.WithUser(ctx, userID)
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	system := state.system
	s.mu.Unlock()
	if system == nil {
		return schema.GetSystemBufferResponse{Buffer: schema.SystemBufferSnapshot{}}, nil
	}
	view := system.Snapshot(req.Limit)
	log.Trace("service system buffer snapshot", "lines", view.TotalLines, "offset", view.ScrollOffset, "limit", req.Limit)
	return schema.GetSystemBufferResponse{
		Buffer: schema.SystemBufferSnapshot{
			Lines:        view.Lines,
			TotalLines:   view.TotalLines,
			ScrollOffset: view.ScrollOffset,
			AtBottom:     view.AtBottom,
		},
	}, nil
}

func (s *service) GetHistory(ctx context.Context, req schema.GetHistoryRequest) (schema.GetHistoryResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.GetHistoryResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	if req.TabID == "" {
		return schema.GetHistoryResponse{}, schema.ErrTabNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	if tab == nil {
		log.Warn("service history get failed", "err", schema.ErrTabNotFound)
		return schema.GetHistoryResponse{}, schema.ErrTabNotFound
	}
	if tab.history == nil {
		tab.history = newHistory(defaultHistoryMax)
	}
	log.Debug("service history fetched", "entries", len(tab.history.Entries()))
	return schema.GetHistoryResponse{Entries: tab.history.Entries()}, nil
}

func (s *service) AppendHistory(ctx context.Context, req schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.AppendHistoryResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	if req.TabID == "" {
		return schema.AppendHistoryResponse{}, schema.ErrTabNotFound
	}
	var entries []string
	changed := false
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	if tab == nil {
		s.mu.Unlock()
		log.Warn("service history append failed", "err", schema.ErrTabNotFound)
		return schema.AppendHistoryResponse{}, schema.ErrTabNotFound
	}
	if tab.history == nil {
		tab.history = newHistory(defaultHistoryMax)
	}
	if tab.history.Append(req.Entry) {
		changed = true
	}
	entries = tab.history.Entries()
	s.mu.Unlock()
	if changed {
		s.persistUser(log, userID)
	}
	log.Debug("service history appended", "changed", changed, "entries", len(entries))
	return schema.AppendHistoryResponse{Entries: entries}, nil
}

// RegisterCommand tracks a running shell command for the tab.
func (s *service) RegisterCommand(ctx context.Context, userID schema.UserID, tabID schema.TabID, handle CommandHandle, cancel context.CancelFunc) {
	if handle == nil || strings.TrimSpace(string(tabID)) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	log := logx.WithUserTab(ctx, userID, tabID)
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[tabID]
	if tab == nil {
		s.mu.Unlock()
		return
	}
	tab.commands = append(tab.commands, commandRun{handle: handle, cancel: cancel})
	count := len(tab.commands)
	s.mu.Unlock()
	log.Debug("service command registered", "running", count)
}

// UnregisterCommand removes a completed shell command from tracking.
func (s *service) UnregisterCommand(userID schema.UserID, tabID schema.TabID, handle CommandHandle) {
	if handle == nil || strings.TrimSpace(string(tabID)) == "" {
		return
	}
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[tabID]
	if tab == nil || len(tab.commands) == 0 {
		s.mu.Unlock()
		return
	}
	next := tab.commands[:0]
	for _, entry := range tab.commands {
		if entry.handle == handle {
			continue
		}
		next = append(next, entry)
	}
	if len(next) == 0 {
		tab.commands = nil
	} else {
		tab.commands = next
	}
	s.mu.Unlock()
}

func (s *service) SaveCodexAuth(ctx context.Context, req schema.SaveCodexAuthRequest) (schema.SaveCodexAuthResponse, error) {
	if ctx == nil {
		return schema.SaveCodexAuthResponse{}, errors.New("missing context")
	}
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.SaveCodexAuthResponse{}, err
	}
	payload := bytesTrimSpace(req.AuthJSON)
	if len(payload) == 0 {
		return schema.SaveCodexAuthResponse{}, fmt.Errorf("%w: auth.json is required", schema.ErrInvalidRequest)
	}
	if !json.Valid(payload) {
		return schema.SaveCodexAuthResponse{}, schema.ErrInvalidCodexAuth
	}
	stateDir := strings.TrimSpace(s.cfg.StateDir)
	if stateDir == "" {
		return schema.SaveCodexAuthResponse{}, errors.New("state directory is required")
	}
	authPath := userhome.AuthPath(stateDir, string(userID))
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		return schema.SaveCodexAuthResponse{}, err
	}
	if err := os.WriteFile(authPath, payload, 0o600); err != nil {
		return schema.SaveCodexAuthResponse{}, err
	}
	return schema.SaveCodexAuthResponse{}, nil
}

func bytesTrimSpace(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	start := 0
	end := len(data)
	for start < end {
		if data[start] != ' ' && data[start] != '\n' && data[start] != '\r' && data[start] != '\t' {
			break
		}
		start++
	}
	for end > start {
		b := data[end-1]
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			break
		}
		end--
	}
	return data[start:end]
}

func (s *service) GetTabUsage(ctx context.Context, req schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error) {
	_ = ctx
	userID, err := normalizeUserID(req.UserID)
	if err != nil {
		return schema.GetTabUsageResponse{}, err
	}
	log := logx.WithUserTab(ctx, userID, req.TabID)
	if req.TabID == "" {
		return schema.GetTabUsageResponse{}, schema.ErrTabNotFound
	}
	s.mu.Lock()
	state := s.getOrCreateUserStateLocked(userID)
	tab := state.tabs[req.TabID]
	var usage *schema.TurnUsage
	if tab != nil && tab.LastUsage != nil {
		usageCopy := *tab.LastUsage
		usage = &usageCopy
	}
	s.mu.Unlock()
	if tab == nil {
		log.Warn("service tab usage failed", "err", schema.ErrTabNotFound)
		return schema.GetTabUsageResponse{}, schema.ErrTabNotFound
	}
	log.Debug("service tab usage fetched", "has_usage", usage != nil)
	return schema.GetTabUsageResponse{Usage: usage}, nil
}

func (s *service) consumeEvents(ctx context.Context, userID schema.UserID, tabID schema.TabID, handle RunHandle, cancel context.CancelFunc, started time.Time) {
	log := logx.WithUserTab(ctx, userID, tabID)
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	log.Info("service exec stream start")
	stream := handle.Events()
	workedInserted := false
	eventCount := 0
	var seenCommandIDs map[string]bool
	lastCommand := ""
	lastCommandEvent := false
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Warn("service exec stream error", "err", err)
			s.appendErrorLine(log, userID, tabID, fmt.Errorf("stream error: %w", err))
			break
		}
		eventCount++
		if !workedInserted && event.Type == schema.EventItemCompleted && event.Item != nil && event.Item.Type == schema.ItemAgentMessage {
			s.appendLine(log, userID, tabID, formatWorkedForLine(time.Since(started)))
			workedInserted = true
		}
		if event.ThreadID != "" {
			if s.setSessionID(userID, tabID, event.ThreadID) {
				s.persistUser(log, userID)
				log.Debug("service session captured", "session", event.ThreadID)
			}
		}
		if event.Type == schema.EventTurnFailed {
			if event.Error != nil && event.Error.Message != "" {
				log.Warn("service exec turn failed", "message", event.Error.Message)
			} else {
				log.Warn("service exec turn failed")
			}
		}
		if event.Type == schema.EventError {
			if event.Message != "" {
				log.Warn("service exec error", "message", event.Message)
			} else {
				log.Warn("service exec error")
			}
		}
		if event.Type == schema.EventTurnCompleted && event.Usage != nil {
			usageCopy := *event.Usage
			s.mu.Lock()
			if state := s.userTabs[userID]; state != nil {
				if tab := state.tabs[tabID]; tab != nil {
					tab.LastUsage = &usageCopy
				}
			}
			s.mu.Unlock()
		}
		if event.Item != nil && event.Item.Type == schema.ItemCommandExecution && event.Item.Command != "" {
			if event.Item.ID != "" {
				if seenCommandIDs == nil {
					seenCommandIDs = make(map[string]bool)
				}
				if seenCommandIDs[event.Item.ID] {
					event.Item.Command = ""
				} else {
					seenCommandIDs[event.Item.ID] = true
				}
			} else if lastCommandEvent && event.Item.Command == lastCommand && event.Type != schema.EventItemStarted {
				event.Item.Command = ""
			} else {
				lastCommand = event.Item.Command
				lastCommandEvent = true
			}
		} else {
			lastCommand = ""
			lastCommandEvent = false
		}
		lines, err := s.renderer.FormatEvent(event)
		if err != nil {
			itemType := ""
			if event.Item != nil {
				itemType = string(event.Item.Type)
			}
			log.Warn("service render failed", "type", event.Type, "item_type", itemType, "err", err)
			s.appendLine(log, userID, tabID, fmt.Sprintf("render error: %v", err))
			continue
		}
		if event.Item != nil && event.Item.Type == schema.ItemCommandExecution {
			lines = trimCommandLines(ctx, lines)
		}
		if len(lines) > 0 {
			s.appendLines(log, userID, tabID, lines)
		}
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		log.Warn("service exec wait failed", "err", err)
		s.appendErrorLine(log, userID, tabID, err)
	} else if result.ExitCode != 0 {
		s.appendErrorLine(log, userID, tabID, fmt.Errorf("run exited with code %d", result.ExitCode))
	}
	if err := handle.Close(); err != nil {
		log.Warn("service exec close failed", "err", err)
		s.appendErrorLine(log, userID, tabID, fmt.Errorf("runner close failed: %w", err))
	}

	if err == nil {
		log.Info("service exec finished", "exit_code", result.ExitCode, "events", eventCount, "duration_ms", time.Since(started).Milliseconds())
	}
	s.mu.Lock()
	state := s.userTabs[userID]
	var event *schema.TabEvent
	if state != nil {
		tab := state.tabs[tabID]
		if tab != nil && tab.Run == handle {
			active := activeTabFromContext(ctx, state)
			tab.Status = schema.TabStatusIdle
			tab.Run = nil
			tab.RunCancel = nil
			tabEvent := schema.TabEvent{
				UserID:    userID,
				Type:      schema.TabEventStatus,
				Tab:       tab.Snapshot(tabID == active),
				ActiveTab: active,
			}
			event = &tabEvent
		}
	}
	s.mu.Unlock()
	if event != nil {
		s.emitTabEvent(*event)
	}
}

const maxCommandLinesTerse = 5

func trimCommandLines(ctx context.Context, lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	if prefs := sessionprefs.FromContext(ctx); prefs != nil && prefs.FullCommandOutput {
		return lines
	}
	if len(lines) > maxCommandLinesTerse {
		return lines[:maxCommandLinesTerse]
	}
	return lines
}

func (s *service) appendErrorLine(log pslog.Logger, userID schema.UserID, tabID schema.TabID, err error) {
	if err == nil {
		return
	}
	var runnerErr *RunnerError
	if errors.As(err, &runnerErr) {
		line, hints := runnerErrorLines(runnerErr)
		s.appendUserLine(log, userID, tabID, line)
		for _, hint := range hints {
			s.appendUserLine(log, userID, tabID, hint)
		}
		return
	}
	s.appendUserLine(log, userID, tabID, fmt.Sprintf("error: %v", err))
}

func runnerErrorLines(err *RunnerError) (string, []string) {
	if err == nil {
		return "error: runner failed", nil
	}
	switch err.Kind {
	case RunnerErrorUnauthorized:
		return "error: runner authentication failed", []string{
			"hint: run `codex login` to refresh credentials",
			"hint: ensure .codex/auth.json is available inside the runner container",
		}
	case RunnerErrorPermissionDenied:
		return "error: runner permission denied", []string{
			"hint: check file permissions and SSH key access inside the runner",
		}
	case RunnerErrorUnavailable:
		return "error: runner unavailable", []string{
			"hint: check that the runner container is running and reachable",
		}
	case RunnerErrorTimeout:
		return "error: runner timed out", []string{
			"hint: retry or check runner health",
		}
	case RunnerErrorCanceled:
		return "error: runner canceled", nil
	case RunnerErrorContainerStart:
		return "error: runner container failed to start", []string{
			"hint: check container runtime logs (podman/containerd) for details",
		}
	case RunnerErrorContainerSocket:
		return "error: runner socket did not become ready", []string{
			"hint: check runner logs for startup failures",
		}
	case RunnerErrorExec:
		return "error: runner exec failed", nil
	case RunnerErrorCommand:
		return "error: runner command failed", nil
	default:
		return fmt.Sprintf("error: %v", err), nil
	}
}

func (s *service) appendUserLine(log pslog.Logger, userID schema.UserID, tabID schema.TabID, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if tabID == "" {
		s.appendSystemLines(log, userID, []string{line})
		return
	}
	s.appendLines(log, userID, tabID, []string{line})
}

func (s *service) setSessionID(userID schema.UserID, tabID schema.TabID, sessionID schema.SessionID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.userTabs[userID]
	if state == nil {
		return false
	}
	tab := state.tabs[tabID]
	if tab == nil {
		return false
	}
	if tab.SessionID == "" {
		tab.SessionID = sessionID
		return true
	}
	return false
}

func (s *service) appendLine(log pslog.Logger, userID schema.UserID, tabID schema.TabID, line string) {
	s.appendLines(log, userID, tabID, []string{line})
}

func formatWorkedForLine(duration time.Duration) string {
	return schema.WorkedForMarker + "Worked for " + formatWorkedDuration(duration)
}

func formatWorkedDuration(duration time.Duration) string {
	if duration < time.Second {
		if duration < 0 {
			duration = 0
		}
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	if duration < time.Minute {
		seconds := int(duration.Round(time.Second).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%ds", seconds)
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("%dm", minutes)
	}
	hours := int(duration.Hours())
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("%dh", hours)
}

func (s *service) appendLines(log pslog.Logger, userID schema.UserID, tabID schema.TabID, lines []string) {
	s.mu.Lock()
	state := s.userTabs[userID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	tab := state.tabs[tabID]
	if tab == nil || tab.buffer == nil {
		s.mu.Unlock()
		return
	}
	tab.buffer.Append(lines...)
	s.mu.Unlock()
	s.emitOutput(userID, tabID, lines)
	s.persistUser(log, userID)
	if log != nil {
		log.Trace("service output appended", "lines", len(lines))
	}
}

func (s *service) appendSystemLines(log pslog.Logger, userID schema.UserID, lines []string) {
	if len(lines) == 0 {
		return
	}
	s.mu.Lock()
	state := s.userTabs[userID]
	if state == nil || state.system == nil {
		s.mu.Unlock()
		return
	}
	state.system.Append(lines...)
	s.mu.Unlock()
	s.emitSystemOutput(userID, lines)
	s.persistUser(log, userID)
	if log != nil {
		log.Trace("service system output appended", "lines", len(lines))
	}
}

func (s *service) emitOutput(userID schema.UserID, tabID schema.TabID, lines []string) {
	if s.sink == nil || len(lines) == 0 {
		return
	}
	s.sink.OnOutput(schema.OutputEvent{
		UserID: userID,
		TabID:  tabID,
		Lines:  append([]string(nil), lines...),
	})
}

func (s *service) emitSystemOutput(userID schema.UserID, lines []string) {
	if s.sink == nil || len(lines) == 0 {
		return
	}
	s.sink.OnSystemOutput(schema.SystemOutputEvent{
		UserID: userID,
		Lines:  append([]string(nil), lines...),
	})
}

func (s *service) emitTabEvent(event schema.TabEvent) {
	if s.sink == nil {
		return
	}
	s.sink.OnTabEvent(event)
}

func (s *service) getOrCreateUserStateLocked(userID schema.UserID) *userState {
	entry := s.userTabs[userID]
	if entry == nil {
		entry = s.loadUserStateLocked(userID)
		s.userTabs[userID] = entry
	}
	if entry.system == nil {
		entry.system = newBufferWithMaxLines(s.cfg.BufferMaxLines)
	}
	if entry.theme == "" {
		entry.theme = s.cfg.DefaultTheme
	}
	return entry
}

func (s *service) loadUserStateLocked(userID schema.UserID) *userState {
	if s.store == nil {
		return &userState{tabs: make(map[schema.TabID]*tab), system: newBufferWithMaxLines(s.cfg.BufferMaxLines), theme: s.cfg.DefaultTheme}
	}
	log := s.logger
	if log != nil {
		log = log.With("user", userID)
	}
	snapshot, ok, err := s.store.Load(userID)
	if err != nil || !ok {
		if err != nil {
			log.Warn("service state load failed", "err", err)
		} else {
			log.Debug("service state missing")
		}
		return &userState{tabs: make(map[schema.TabID]*tab), system: newBufferWithMaxLines(s.cfg.BufferMaxLines), theme: s.cfg.DefaultTheme}
	}
	log.Debug("service state loaded", "tabs", len(snapshot.Tabs))
	loaded := &userState{
		tabs:   make(map[schema.TabID]*tab),
		order:  make([]schema.TabID, 0, len(snapshot.Order)),
		system: newBufferFromPersistedWithMaxLines(persistedBuffer{Lines: snapshot.System.Lines, ScrollOffset: snapshot.System.ScrollOffset}, s.cfg.BufferMaxLines),
		theme:  snapshot.Theme,
	}
	for _, snap := range snapshot.Tabs {
		loaded.tabs[snap.ID] = &tab{
			ID:        snap.ID,
			Name:      snap.Name,
			Repo:      snap.Repo,
			Model:     snap.Model,
			SessionID: snap.SessionID,
			Status:    schema.TabStatusIdle,
			buffer:    newBufferFromPersistedWithMaxLines(persistedBuffer{Lines: snap.Buffer.Lines, ScrollOffset: snap.Buffer.ScrollOffset}, s.cfg.BufferMaxLines),
			history:   newHistoryFromPersisted(snap.History),
		}
	}
	for _, id := range snapshot.Order {
		if _, ok := loaded.tabs[id]; ok {
			loaded.order = append(loaded.order, id)
		}
	}
	if len(loaded.order) == 0 {
		for _, snap := range snapshot.Tabs {
			loaded.order = append(loaded.order, snap.ID)
		}
	}
	if loaded.theme == "" {
		loaded.theme = s.cfg.DefaultTheme
	}
	return loaded
}

func (s *service) persistUser(log pslog.Logger, userID schema.UserID) {
	if s.store == nil {
		return
	}
	snapshot, ok := s.snapshotUser(userID)
	if !ok {
		if log != nil {
			log.Debug("service persist skipped", "reason", "missing state")
		}
		return
	}
	if err := s.store.Save(userID, snapshot); err != nil {
		if log != nil {
			log.Warn("service persist failed", "err", err)
		}
		return
	}
	if log != nil {
		log.Trace("service state persisted", "tabs", len(snapshot.Tabs))
	}
}

func (s *service) snapshotUser(userID schema.UserID) (persist.UserSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userState := s.userTabs[userID]
	if userState == nil {
		return persist.UserSnapshot{}, false
	}
	tabs := make([]persist.TabSnapshot, 0, len(userState.tabs))
	for _, id := range userState.order {
		tab := userState.tabs[id]
		if tab == nil {
			continue
		}
		buffer := persistedBuffer{}
		if tab.buffer != nil {
			buffer = tab.buffer.Export()
		}
		history := []string(nil)
		if tab.history != nil {
			history = tab.history.Entries()
		}
		tabs = append(tabs, persist.TabSnapshot{
			ID:        tab.ID,
			Name:      tab.Name,
			Repo:      tab.Repo,
			Model:     tab.Model,
			SessionID: tab.SessionID,
			Buffer: persist.BufferSnapshot{
				Lines:        buffer.Lines,
				ScrollOffset: buffer.ScrollOffset,
			},
			History: history,
		})
	}
	order := append([]schema.TabID(nil), userState.order...)
	system := persistedBuffer{}
	if userState.system != nil {
		system = userState.system.Export()
	}
	return persist.UserSnapshot{
		Order: order,
		Tabs:  tabs,
		System: persist.BufferSnapshot{
			Lines:        system.Lines,
			ScrollOffset: system.ScrollOffset,
		},
		Theme: userState.theme,
	}, true
}

func activeTabFromContext(ctx context.Context, state *userState) schema.TabID {
	if ctx == nil {
		return ""
	}
	prefs := sessionprefs.FromContext(ctx)
	if prefs == nil {
		return ""
	}
	if state == nil || state.tabs == nil || len(state.tabs) == 0 {
		prefs.ActiveTab = ""
		return ""
	}
	active := prefs.ActiveTab
	if active != "" {
		if _, ok := state.tabs[active]; ok {
			return active
		}
		prefs.ActiveTab = ""
	}
	if len(state.order) > 0 {
		first := state.order[0]
		if _, ok := state.tabs[first]; ok {
			prefs.ActiveTab = first
			return first
		}
	}
	for id := range state.tabs {
		prefs.ActiveTab = id
		return id
	}
	return ""
}

func detachRunContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		if logger := pslog.Ctx(ctx); logger != nil {
			base = logx.CopyContextFields(pslog.ContextWithLogger(base, logger), ctx)
		}
		if prefs := sessionprefs.FromContext(ctx); prefs != nil {
			copyPrefs := *prefs
			base = sessionprefs.WithContext(base, &copyPrefs)
		}
	}
	return context.WithCancel(base)
}

func normalizeUserID(userID schema.UserID) (schema.UserID, error) {
	if err := schema.ValidateUserID(userID); err != nil {
		return "", schema.ErrInvalidUser
	}
	return userID, nil
}

func formatTabName(name string, max int, suffix string) string {
	if max <= 0 {
		return name
	}
	if len(name) <= max {
		return name
	}
	cut := max - len(suffix)
	if cut < 1 {
		return name[:max]
	}
	return name[:cut] + suffix
}

func removeTabID(order []schema.TabID, id schema.TabID) []schema.TabID {
	for i, current := range order {
		if current == id {
			return append(order[:i], order[i+1:]...)
		}
	}
	return order
}

func mapBufferSnapshot(tabID schema.TabID, view bufferView) schema.BufferSnapshot {
	return schema.BufferSnapshot{
		TabID:        tabID,
		Lines:        view.Lines,
		TotalLines:   view.TotalLines,
		ScrollOffset: view.ScrollOffset,
		AtBottom:     view.AtBottom,
	}
}
