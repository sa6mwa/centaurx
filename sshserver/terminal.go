package sshserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	gliderssh "github.com/gliderlabs/ssh"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/eventbus"
	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

type terminalSession struct {
	sess       gliderssh.Session
	service    core.Service
	handler    CommandHandler
	authStore  LoginAuthStore
	userID     schema.UserID
	promptIdle string
	screen     *screen
	ctx        context.Context
	events     <-chan eventbus.Event

	width  int
	height int

	tabs           []schema.TabSnapshot
	activeTab      schema.TabID
	tabWindowStart int
	buffer         schema.BufferSnapshot
	system         schema.SystemBufferSnapshot
	tabStatus      map[schema.TabID]schema.TabStatus
	queues         map[schema.TabID][]string
	themeName      schema.ThemeName

	editor         lineEditor
	notice         string
	spinnerIdx     int
	running        bool
	commandActive  atomic.Int32
	commandSpinner atomic.Bool
	dirty          bool
	redrawCh       chan struct{}

	history      []string
	historyIndex int
	historyDirty bool
	historyTabID schema.TabID

	chpasswd  *chpasswdState
	codexauth *codexAuthState
	rotateSSH *rotateSSHKeyState
}

type chpasswdStep int

const (
	chpasswdStepCurrent chpasswdStep = iota
	chpasswdStepNew
	chpasswdStepConfirm
	chpasswdStepTOTP
)

type chpasswdState struct {
	step        chpasswdStep
	current     string
	totp        string
	newPassword string
}

func (c *chpasswdState) prompt() string {
	if c == nil {
		return ""
	}
	switch c.step {
	case chpasswdStepCurrent:
		return "current password: "
	case chpasswdStepNew:
		return "new password: "
	case chpasswdStepConfirm:
		return "confirm new password: "
	case chpasswdStepTOTP:
		return "totp: "
	default:
		return "> "
	}
}

type codexAuthState struct{}

func (c *codexAuthState) prompt() string {
	return "auth.json: "
}

type rotateSSHKeyState struct{}

func (r *rotateSSHKeyState) prompt() string {
	return "type YES to rotate SSH key: "
}

func newTerminalSession(sess gliderssh.Session, service core.Service, handler CommandHandler, authStore LoginAuthStore, userID schema.UserID, idlePrompt string, events <-chan eventbus.Event) *terminalSession {
	return &terminalSession{
		sess:         sess,
		service:      service,
		handler:      handler,
		authStore:    authStore,
		userID:       userID,
		promptIdle:   idlePrompt,
		screen:       newScreen(sess),
		events:       events,
		tabStatus:    make(map[schema.TabID]schema.TabStatus),
		queues:       make(map[schema.TabID][]string),
		historyIndex: -1,
		redrawCh:     make(chan struct{}, 1),
	}
}

func (t *terminalSession) log() pslog.Logger {
	ctx := t.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return pslog.Ctx(ctx).With("user", t.userID)
}

func (t *terminalSession) logTab(tabID schema.TabID) pslog.Logger {
	log := t.log()
	if tabID != "" {
		log = log.With("tab", tabID)
	}
	return log
}

func (t *terminalSession) SetSize(width, height int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	t.width = width
	t.height = height
}

func (t *terminalSession) Run(ctx context.Context, winCh <-chan gliderssh.Window) error {
	if ctx == nil {
		ctx = context.Background()
	}
	t.ctx = sessionprefs.WithContext(ctx, sessionprefs.New())
	defer t.saveHistoryOnExit()
	t.screen.EnterAltScreen()
	defer t.screen.ExitAltScreen()

	t.refreshState()
	t.render()
	t.log().Info("tui session start", "width", t.width, "height", t.height)

	keys := make(chan key, 16)
	go readKeys(t.sess, keys)

	stateTicker := time.NewTicker(2 * time.Second)
	spinnerTicker := time.NewTicker(250 * time.Millisecond)
	defer stateTicker.Stop()
	defer spinnerTicker.Stop()

	events := t.events

	for {
		select {
		case <-ctx.Done():
			return nil
		case k, ok := <-keys:
			if !ok {
				return nil
			}
			if t.handleKey(k) {
				return nil
			}
		case win, ok := <-winCh:
			if ok {
				t.SetSize(win.Width, win.Height)
				t.refreshBuffer()
				t.dirty = true
				t.log().Debug("tui resize", "width", t.width, "height", t.height)
			}
		case ev, ok := <-events:
			if !ok {
				events = nil
				break
			}
			t.handleEvent(ev)
		case <-spinnerTicker.C:
			if t.running || t.commandSpinner.Load() {
				t.spinnerIdx = (t.spinnerIdx + 1) % len(spinnerFrames)
				t.dirty = true
			}
		case <-t.redrawCh:
			t.dirty = true
		case <-stateTicker.C:
			t.refreshState()
		}

		if t.dirty {
			t.render()
			t.dirty = false
		}
	}
}

func (t *terminalSession) handleEvent(ev eventbus.Event) {
	switch ev.Type {
	case eventbus.EventOutput:
		if ev.Output.TabID != t.activeTab {
			return
		}
		if t.refreshBuffer() {
			t.dirty = true
		}
	case eventbus.EventSystemOutput:
		if t.activeTab != "" {
			return
		}
		if t.refreshBuffer() {
			t.dirty = true
		}
	case eventbus.EventTab:
		t.refreshState()
	}
}

func (t *terminalSession) handleKey(k key) bool {
	if t.codexauth != nil {
		return t.handleCodexAuthKey(k)
	}
	if t.chpasswd != nil {
		return t.handleChpasswdKey(k)
	}
	if t.rotateSSH != nil {
		return t.handleRotateSSHKeyKey(k)
	}
	switch k.kind {
	case keyCtrlD:
		if t.editor.Len() == 0 {
			t.log().Info("tui exit", "reason", "ctrl-d")
			_ = t.sess.Exit(0)
			return true
		}
		t.cancelScroll()
		t.editor.Delete()
	case keyCtrlC:
		t.editor.Clear()
		t.historyDirty = true
	case keyEnter:
		if t.handleEnter() {
			return true
		}
	case keyCtrlJ:
		t.cancelScroll()
		t.editor.InsertRune('\n')
		t.historyDirty = true
	case keyRune:
		t.cancelScroll()
		t.editor.InsertRune(k.r)
		t.historyDirty = true
	case keyBackspace:
		t.cancelScroll()
		t.editor.Backspace()
		t.historyDirty = true
	case keyDelete:
		t.cancelScroll()
		t.editor.Delete()
		t.historyDirty = true
	case keyLeft:
		t.editor.MoveLeft()
	case keyRight:
		t.editor.MoveRight()
	case keyHome, keyCtrlA:
		t.editor.MoveStart()
	case keyEnd, keyCtrlE:
		t.editor.MoveEnd()
	case keyAltB:
		t.editor.MoveWordLeft()
	case keyAltF:
		t.editor.MoveWordRight()
	case keyCtrlW:
		t.cancelScroll()
		t.editor.DeleteWordBackward()
		t.historyDirty = true
	case keyCtrlU:
		t.cancelScroll()
		t.editor.KillLineStart()
		t.historyDirty = true
	case keyCtrlK:
		t.cancelScroll()
		t.editor.KillLineEnd()
		t.historyDirty = true
	case keyTab:
		t.cycleTab(1)
	case keyShiftTab:
		t.cycleTab(-1)
	case keyUp:
		if t.editor.cursor == 0 || t.editor.cursor == t.editor.Len() {
			t.historyUp()
		} else {
			t.editor.MoveUp()
		}
	case keyDown:
		if t.editor.cursor == 0 || t.editor.cursor == t.editor.Len() {
			t.historyDown()
		} else {
			t.editor.MoveDown()
		}
	case keyPageUp:
		t.scroll(1)
	case keyPageDown:
		t.scroll(-1)
	}
	t.dirty = true
	return false
}

func (t *terminalSession) handleChpasswdKey(k key) bool {
	switch k.kind {
	case keyCtrlC:
		t.cancelChpasswd()
	case keyEnter:
		t.submitChpasswdField()
	case keyCtrlA, keyHome:
		t.editor.MoveStart()
	case keyCtrlE, keyEnd:
		t.editor.MoveEnd()
	case keyAltB:
		t.editor.MoveWordLeft()
	case keyAltF:
		t.editor.MoveWordRight()
	case keyCtrlW:
		t.editor.DeleteWordBackward()
	case keyCtrlU:
		t.editor.KillLineStart()
	case keyCtrlK:
		t.editor.KillLineEnd()
	case keyLeft:
		t.editor.MoveLeft()
	case keyRight:
		t.editor.MoveRight()
	case keyBackspace:
		t.editor.Backspace()
	case keyDelete:
		t.editor.Delete()
	case keyRune:
		t.editor.InsertRune(k.r)
	case keyCtrlJ, keyTab, keyShiftTab, keyUp, keyDown, keyPageUp, keyPageDown, keyCtrlD:
		// Ignore navigation/control keys during password entry.
	}
	t.dirty = true
	return false
}

func (t *terminalSession) handleEnter() bool {
	raw := t.editor.String()
	t.saveHistoryEntry(raw)
	line := strings.TrimSpace(raw)
	t.editor.Clear()
	t.historyIndex = -1
	t.historyDirty = false
	t.notice = ""
	if line == "" {
		return false
	}

	isMultiline := strings.Contains(raw, "\n")
	if !isMultiline {
		if line == "/exit" || line == "/quit" || line == "/logout" || line == "/q" {
			t.log().Info("tui exit", "reason", "command", "input", line)
			_ = t.sess.Exit(0)
			return true
		}

		if isChpasswdCommand(line) {
			t.startChpasswd()
			return false
		}
		if isCodexAuthCommand(line) {
			t.startCodexAuth()
			return false
		}
		if isRotateSSHKeyCommand(line) {
			t.startRotateSSHKey()
			return false
		}
		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, "!") {
			t.logTab(t.activeTab).Debug("tui command", "input", line)
			if isStatusCommand(line) || isNewCommand(line) || strings.HasPrefix(line, "!") {
				t.runCommandAsync(line)
				return false
			}
			if err := t.handleCommand(line); err != nil {
				t.appendError(t.activeTab, err)
			}
			t.refreshState()
			return false
		}
	}

	if t.activeTab == "" {
		t.log().Warn("tui prompt rejected", "reason", "no active tab")
		t.appendNotice("no active tab; use /new <repo>")
		return false
	}

	if t.tabStatus[t.activeTab] == schema.TabStatusRunning {
		t.logTab(t.activeTab).Debug("tui prompt queued", "len", len(raw))
		t.queuePrompt(t.activeTab, raw)
		return false
	}

	if err := t.sendPrompt(t.activeTab, raw); err != nil {
		if errors.Is(err, schema.ErrTabBusy) {
			t.logTab(t.activeTab).Debug("tui prompt queued", "reason", "busy")
			t.queuePrompt(t.activeTab, raw)
			return false
		}
		t.appendError(t.activeTab, err)
	}
	return false
}

func (t *terminalSession) handleCommand(line string) error {
	if t.handler == nil {
		return fmt.Errorf("commands unavailable")
	}
	tabID := t.activeTab
	handled, err := t.handler.Handle(t.ctx, t.userID, tabID, line)
	if err != nil {
		t.logTab(tabID).Warn("tui command failed", "err", err)
		return err
	}
	if !handled {
		t.logTab(tabID).Warn("tui command unknown", "input", line)
		return fmt.Errorf("unknown command")
	}
	t.logTab(tabID).Trace("tui command handled", "input", line)
	return nil
}

func (t *terminalSession) startChpasswd() {
	if t.authStore == nil {
		t.appendError(t.activeTab, errors.New("password change unavailable"))
		return
	}
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui chpasswd start", "command", "/chpasswd")
	t.chpasswd = &chpasswdState{step: chpasswdStepCurrent}
	t.editor.Clear()
	t.historyDirty = true
	t.notice = ""
	t.requestRedraw()
}

func (t *terminalSession) startCodexAuth() {
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui codexauth start", "command", "/codexauth")
	t.codexauth = &codexAuthState{}
	t.editor.Clear()
	t.historyDirty = true
	t.notice = "paste auth.json, then submit a blank line or press Ctrl+D"
	t.requestRedraw()
}

func (t *terminalSession) startRotateSSHKey() {
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui rotatesshkey start", "command", "/rotatesshkey")
	t.rotateSSH = &rotateSSHKeyState{}
	t.editor.Clear()
	t.historyDirty = true
	t.notice = ""
	t.requestRedraw()
}

func (t *terminalSession) cancelRotateSSHKey() {
	if t.rotateSSH == nil {
		return
	}
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui rotatesshkey cancel", "command", "/rotatesshkey")
	t.rotateSSH = nil
	t.editor.Clear()
	t.notice = ""
	t.appendMessage(t.activeTab, "ssh key rotation cancelled")
	t.requestRedraw()
}

func (t *terminalSession) submitRotateSSHKey() {
	if t.rotateSSH == nil {
		return
	}
	value := strings.TrimSpace(t.editor.String())
	t.editor.Clear()
	t.rotateSSH = nil
	t.notice = ""
	if value != "YES" {
		t.appendMessage(t.activeTab, "ssh key rotation cancelled")
		return
	}
	t.runCommandAsync("/rotatesshkey affirm")
}

func (t *terminalSession) handleRotateSSHKeyKey(k key) bool {
	switch k.kind {
	case keyCtrlC:
		t.cancelRotateSSHKey()
	case keyEnter:
		t.submitRotateSSHKey()
	case keyCtrlA, keyHome:
		t.editor.MoveStart()
	case keyCtrlE, keyEnd:
		t.editor.MoveEnd()
	case keyAltB:
		t.editor.MoveWordLeft()
	case keyAltF:
		t.editor.MoveWordRight()
	case keyCtrlW:
		t.editor.DeleteWordBackward()
	case keyCtrlU:
		t.editor.KillLineStart()
	case keyCtrlK:
		t.editor.KillLineEnd()
	case keyLeft:
		t.editor.MoveLeft()
	case keyRight:
		t.editor.MoveRight()
	case keyBackspace:
		t.editor.Backspace()
	case keyDelete:
		t.editor.Delete()
	case keyRune:
		t.editor.InsertRune(k.r)
	case keyCtrlJ, keyTab, keyUp, keyDown, keyPageUp, keyPageDown, keyCtrlD:
		// Ignore navigation/control keys during confirmation.
	}
	t.dirty = true
	return false
}

func (t *terminalSession) cancelCodexAuth() {
	if t.codexauth == nil {
		return
	}
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui codexauth cancel", "command", "/codexauth")
	t.codexauth = nil
	t.editor.Clear()
	t.notice = ""
	t.appendMessage(t.activeTab, "codex auth upload cancelled")
	t.requestRedraw()
}

func (t *terminalSession) finishCodexAuth() {
	if t.codexauth == nil {
		return
	}
	payload := strings.TrimSpace(t.editor.String())
	t.editor.Clear()
	t.codexauth = nil
	t.notice = ""
	if payload == "" {
		t.appendError(t.activeTab, errors.New("auth.json is required"))
		return
	}
	_, err := t.service.SaveCodexAuth(t.ctx, schema.SaveCodexAuthRequest{
		UserID:   t.userID,
		AuthJSON: []byte(payload),
	})
	if err != nil {
		t.appendError(t.activeTab, err)
		return
	}
	t.appendMessage(t.activeTab, "codex auth updated")
}

func (t *terminalSession) handleCodexAuthKey(k key) bool {
	switch k.kind {
	case keyCtrlC:
		t.cancelCodexAuth()
	case keyCtrlD:
		t.finishCodexAuth()
	case keyEnter, keyCtrlJ:
		if t.codexAuthLineEmpty() {
			t.finishCodexAuth()
			break
		}
		t.cancelScroll()
		t.editor.InsertRune('\n')
	case keyCtrlA, keyHome:
		t.editor.MoveStart()
	case keyCtrlE, keyEnd:
		t.editor.MoveEnd()
	case keyAltB:
		t.editor.MoveWordLeft()
	case keyAltF:
		t.editor.MoveWordRight()
	case keyCtrlW:
		t.cancelScroll()
		t.editor.DeleteWordBackward()
	case keyCtrlU:
		t.cancelScroll()
		t.editor.KillLineStart()
	case keyCtrlK:
		t.cancelScroll()
		t.editor.KillLineEnd()
	case keyLeft:
		t.editor.MoveLeft()
	case keyRight:
		t.editor.MoveRight()
	case keyBackspace:
		t.cancelScroll()
		t.editor.Backspace()
	case keyDelete:
		t.cancelScroll()
		t.editor.Delete()
	case keyRune:
		t.cancelScroll()
		t.editor.InsertRune(k.r)
	case keyTab, keyShiftTab, keyUp, keyDown, keyPageUp, keyPageDown:
		// Ignore navigation keys during codex auth input.
	}
	t.dirty = true
	return false
}

func (t *terminalSession) codexAuthLineEmpty() bool {
	input := t.editor.String()
	if input == "" {
		return true
	}
	idx := strings.LastIndex(input, "\n")
	if idx == -1 {
		return strings.TrimSpace(input) == ""
	}
	return strings.TrimSpace(input[idx+1:]) == ""
}

func (t *terminalSession) cancelChpasswd() {
	if t.chpasswd == nil {
		return
	}
	log := t.log()
	if t.activeTab != "" {
		log = log.With("tab", t.activeTab)
	}
	log.Info("tui chpasswd cancel", "command", "/chpasswd")
	t.chpasswd = nil
	t.editor.Clear()
	t.appendMessage(t.activeTab, "password change cancelled")
	t.requestRedraw()
}

func (t *terminalSession) resetChpasswd() {
	if t.chpasswd == nil {
		return
	}
	t.chpasswd.step = chpasswdStepCurrent
	t.chpasswd.current = ""
	t.chpasswd.totp = ""
	t.chpasswd.newPassword = ""
	t.editor.Clear()
}

func (t *terminalSession) submitChpasswdField() {
	if t.chpasswd == nil {
		return
	}
	value := t.editor.String()
	t.editor.Clear()
	switch t.chpasswd.step {
	case chpasswdStepCurrent:
		if strings.TrimSpace(value) == "" {
			t.appendError(t.activeTab, errors.New("current password is required"))
			return
		}
		t.chpasswd.current = value
		t.chpasswd.step = chpasswdStepNew
	case chpasswdStepTOTP:
		if strings.TrimSpace(value) == "" {
			t.appendError(t.activeTab, errors.New("totp is required"))
			return
		}
		if t.authStore == nil {
			t.appendError(t.activeTab, errors.New("password change unavailable"))
			t.chpasswd = nil
			return
		}
		t.chpasswd.totp = value
		if err := t.authStore.ChangePassword(string(t.userID), t.chpasswd.current, t.chpasswd.totp, t.chpasswd.newPassword); err != nil {
			log := t.log()
			if t.activeTab != "" {
				log = log.With("tab", t.activeTab)
			}
			log.Warn("tui chpasswd failed", "err", err)
			t.appendError(t.activeTab, err)
			t.resetChpasswd()
			return
		}
		log := t.log()
		if t.activeTab != "" {
			log = log.With("tab", t.activeTab)
		}
		log.Info("tui chpasswd ok", "command", "/chpasswd")
		t.appendMessage(t.activeTab, "password updated")
		t.chpasswd = nil
	case chpasswdStepNew:
		if strings.TrimSpace(value) == "" {
			t.appendError(t.activeTab, errors.New("new password is required"))
			return
		}
		t.chpasswd.newPassword = value
		t.chpasswd.step = chpasswdStepConfirm
	case chpasswdStepConfirm:
		if value != t.chpasswd.newPassword {
			t.appendError(t.activeTab, errors.New("passwords do not match"))
			t.chpasswd.newPassword = ""
			t.chpasswd.step = chpasswdStepNew
			return
		}
		t.chpasswd.step = chpasswdStepTOTP
	}
}

func (t *terminalSession) runCommandAsync(line string) {
	stopSpinner := t.startCommandSpinner(commandSpinnerDelay)
	tabID := t.activeTab
	t.logTab(tabID).Debug("tui command async start", "input", line)
	go func() {
		defer stopSpinner()
		if t.handler == nil {
			t.appendAsyncError(tabID, errors.New("commands unavailable"))
			return
		}
		handled, err := t.handler.Handle(t.ctx, t.userID, tabID, line)
		if err != nil {
			t.appendAsyncError(tabID, err)
			return
		}
		if !handled {
			t.appendAsyncError(tabID, errors.New("unknown command"))
		}
	}()
}

func (t *terminalSession) startCommandSpinner(delay time.Duration) func() {
	t.commandActive.Add(1)
	timer := time.AfterFunc(delay, func() {
		if t.commandActive.Load() > 0 {
			t.commandSpinner.Store(true)
			t.requestRedraw()
		}
	})
	var stopped atomic.Bool
	return func() {
		if stopped.Swap(true) {
			return
		}
		timer.Stop()
		if t.commandActive.Add(-1) <= 0 {
			t.commandActive.Store(0)
			t.commandSpinner.Store(false)
			t.requestRedraw()
		}
	}
}

func (t *terminalSession) requestRedraw() {
	select {
	case t.redrawCh <- struct{}{}:
	default:
	}
}

func (t *terminalSession) appendAsyncError(tabID schema.TabID, err error) {
	if err == nil {
		return
	}
	t.logTab(tabID).Warn("tui command async error", "err", err)
	if tabID == "" {
		_, _ = t.service.AppendSystemOutput(t.ctx, schema.AppendSystemOutputRequest{
			UserID: t.userID,
			Lines:  []string{fmt.Sprintf("error: %v", err)},
		})
		return
	}
	_, _ = t.service.AppendOutput(t.ctx, schema.AppendOutputRequest{
		UserID: t.userID,
		TabID:  tabID,
		Lines:  []string{fmt.Sprintf("error: %v", err)},
	})
}

func (t *terminalSession) refreshState() {
	prevTabs := t.tabs
	prevActive := t.activeTab
	prevStatus := t.tabStatus
	prevRunning := t.running
	prevTheme := t.themeName
	resp, err := t.service.ListTabs(t.ctx, schema.ListTabsRequest{UserID: t.userID})
	if err != nil {
		t.log().Warn("tui refresh state failed", "err", err)
		return
	}
	t.tabs = resp.Tabs
	t.activeTab = resp.ActiveTab
	t.themeName = resp.Theme
	if t.themeName == "" {
		t.themeName = schema.DefaultTheme
	}
	t.tabStatus = make(map[schema.TabID]schema.TabStatus, len(resp.Tabs))
	for _, tab := range resp.Tabs {
		t.tabStatus[tab.ID] = tab.Status
	}
	t.running = t.tabStatus[t.activeTab] == schema.TabStatusRunning
	bufferChanged := t.refreshBuffer()
	if prevActive != t.activeTab || t.historyTabID != t.activeTab {
		t.refreshHistory()
	}

	for _, tab := range resp.Tabs {
		if tab.Status != schema.TabStatusIdle {
			continue
		}
		queue := t.queues[tab.ID]
		if len(queue) == 0 {
			continue
		}
		prompt := queue[0]
		if err := t.sendPrompt(tab.ID, prompt); err != nil {
			if errors.Is(err, schema.ErrTabBusy) {
				continue
			}
			t.appendError(tab.ID, err)
			continue
		}
		t.queues[tab.ID] = queue[1:]
	}
	stateChanged := bufferChanged ||
		prevActive != t.activeTab ||
		prevRunning != t.running ||
		prevTheme != t.themeName ||
		!tabsEqual(prevTabs, t.tabs) ||
		!tabStatusEqual(prevStatus, t.tabStatus)
	if stateChanged {
		t.logTab(t.activeTab).Trace("tui state updated", "tabs", len(t.tabs), "running", t.running)
		t.dirty = true
	}
}

func (t *terminalSession) refreshHistory() {
	if t.activeTab == "" {
		t.history = nil
		t.historyIndex = -1
		t.historyDirty = false
		t.historyTabID = ""
		return
	}
	if t.activeTab == t.historyTabID {
		return
	}
	resp, err := t.service.GetHistory(t.ctx, schema.GetHistoryRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui history refresh failed", "err", err)
		t.history = nil
		t.historyIndex = -1
		t.historyDirty = false
		t.historyTabID = t.activeTab
		return
	}
	t.history = resp.Entries
	t.historyIndex = -1
	t.historyDirty = false
	t.historyTabID = t.activeTab
	t.logTab(t.activeTab).Trace("tui history refreshed", "entries", len(t.history))
}

func (t *terminalSession) refreshBuffer() bool {
	if t.activeTab == "" {
		resp, err := t.service.GetSystemBuffer(t.ctx, schema.GetSystemBufferRequest{
			UserID: t.userID,
			Limit:  t.viewHeight(),
		})
		if err != nil {
			t.log().Warn("tui system buffer failed", "err", err)
			t.system = schema.SystemBufferSnapshot{}
			t.buffer = schema.BufferSnapshot{}
			return true
		}
		changed := !systemBufferEqual(t.system, resp.Buffer)
		t.system = resp.Buffer
		t.buffer = schema.BufferSnapshot{}
		return changed
	}
	limit := t.viewHeight()
	resp, err := t.service.GetBuffer(t.ctx, schema.GetBufferRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
		Limit:  limit,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui buffer refresh failed", "err", err)
		return false
	}
	changed := !bufferEqual(t.buffer, resp.Buffer)
	t.buffer = resp.Buffer
	return changed
}

func (t *terminalSession) cycleTab(step int) {
	if len(t.tabs) == 0 {
		return
	}
	if step == 0 {
		return
	}
	var next schema.TabSnapshot
	if t.activeTab == "" {
		if step < 0 {
			next = t.tabs[len(t.tabs)-1]
		} else {
			next = t.tabs[0]
		}
	} else {
		activeIndex := 0
		found := false
		for i, tab := range t.tabs {
			if tab.ID == t.activeTab {
				activeIndex = i
				found = true
				break
			}
		}
		if !found {
			activeIndex = -1
		}
		if activeIndex < 0 {
			if step < 0 {
				next = t.tabs[len(t.tabs)-1]
			} else {
				next = t.tabs[0]
			}
		} else {
			nextIndex := activeIndex + step
			for nextIndex < 0 {
				nextIndex += len(t.tabs)
			}
			nextIndex = nextIndex % len(t.tabs)
			next = t.tabs[nextIndex]
		}
	}
	if next.ID == t.activeTab {
		return
	}
	prev := t.activeTab
	_, _ = t.service.ActivateTab(t.ctx, schema.ActivateTabRequest{
		UserID: t.userID,
		TabID:  next.ID,
	})
	t.activeTab = next.ID
	t.refreshState()
	t.logTab(t.activeTab).Debug("tui tab switched", "from", prev, "to", next.ID)
}

func (t *terminalSession) scroll(direction int) {
	if t.activeTab == "" {
		return
	}
	limit := t.viewHeight()
	if limit <= 0 {
		return
	}
	delta := limit * direction
	_, err := t.service.ScrollBuffer(t.ctx, schema.ScrollBufferRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
		Delta:  delta,
		Limit:  limit,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui scroll failed", "err", err)
		return
	}
	t.refreshBuffer()
	t.logTab(t.activeTab).Trace("tui scroll", "delta", delta, "limit", limit)
}

func (t *terminalSession) cancelScroll() {
	if t.activeTab == "" || t.buffer.AtBottom {
		return
	}
	offset := t.buffer.ScrollOffset
	if offset == 0 {
		return
	}
	limit := t.viewHeight()
	_, err := t.service.ScrollBuffer(t.ctx, schema.ScrollBufferRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
		Delta:  -offset,
		Limit:  limit,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui scroll reset failed", "err", err)
		return
	}
	t.refreshBuffer()
	t.logTab(t.activeTab).Trace("tui scroll reset")
}

func (t *terminalSession) historyUp() {
	if t.activeTab == "" {
		return
	}
	appended := t.saveHistoryDraft()
	if len(t.history) == 0 {
		return
	}
	if t.historyIndex == -1 {
		if appended && len(t.history) > 1 {
			t.historyIndex = len(t.history) - 2
		} else {
			t.historyIndex = len(t.history) - 1
		}
	} else if t.historyIndex > 0 {
		t.historyIndex--
	}
	if t.historyIndex >= 0 && t.historyIndex < len(t.history) {
		t.editor.SetString(t.history[t.historyIndex])
		t.historyDirty = false
		t.logTab(t.activeTab).Trace("tui history up", "index", t.historyIndex)
	}
}

func (t *terminalSession) historyDown() {
	if t.activeTab == "" || len(t.history) == 0 {
		return
	}
	t.saveHistoryDraft()
	if t.historyIndex == -1 {
		return
	}
	if t.historyIndex < len(t.history)-1 {
		t.historyIndex++
	}
	if t.historyIndex >= 0 && t.historyIndex < len(t.history) {
		t.editor.SetString(t.history[t.historyIndex])
		t.historyDirty = false
		t.logTab(t.activeTab).Trace("tui history down", "index", t.historyIndex)
	}
}

func (t *terminalSession) saveHistoryDraft() bool {
	if t.activeTab == "" {
		return false
	}
	if t.historyIndex != -1 && !t.historyDirty {
		return false
	}
	entry := t.editor.String()
	if strings.TrimSpace(entry) == "" {
		return false
	}
	appended := len(t.history) == 0 || t.history[len(t.history)-1] != entry
	resp, err := t.service.AppendHistory(t.ctx, schema.AppendHistoryRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
		Entry:  entry,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui history save failed", "err", err)
		return false
	}
	t.history = resp.Entries
	t.historyTabID = t.activeTab
	t.historyDirty = false
	t.logTab(t.activeTab).Trace("tui history saved", "entries", len(t.history))
	return appended
}

func (t *terminalSession) saveHistoryEntry(entry string) {
	if t.activeTab == "" {
		return
	}
	if strings.TrimSpace(entry) == "" {
		return
	}
	resp, err := t.service.AppendHistory(t.ctx, schema.AppendHistoryRequest{
		UserID: t.userID,
		TabID:  t.activeTab,
		Entry:  entry,
	})
	if err != nil {
		t.logTab(t.activeTab).Warn("tui history save failed", "err", err)
		return
	}
	t.history = resp.Entries
	t.historyTabID = t.activeTab
	t.logTab(t.activeTab).Trace("tui history saved", "entries", len(t.history))
}

func (t *terminalSession) saveHistoryOnExit() {
	if t.chpasswd != nil {
		return
	}
	if t.editor.Len() == 0 {
		return
	}
	t.saveHistoryEntry(t.editor.String())
	t.logTab(t.activeTab).Debug("tui history flushed on exit")
}

func (t *terminalSession) queuePrompt(tabID schema.TabID, prompt string) {
	t.queues[tabID] = append(t.queues[tabID], prompt)
	display := prompt
	if strings.Contains(display, "\n") {
		display = strings.ReplaceAll(display, "\n", "\\n")
	}
	_, _ = t.service.AppendOutput(t.ctx, schema.AppendOutputRequest{
		UserID: t.userID,
		TabID:  tabID,
		Lines:  []string{fmt.Sprintf("queued prompt: %s", display)},
	})
	t.logTab(tabID).Debug("tui prompt queued", "queue_len", len(t.queues[tabID]))
}

func (t *terminalSession) sendPrompt(tabID schema.TabID, prompt string) error {
	t.logTab(tabID).Debug("tui prompt send", "len", len(prompt))
	_, err := t.service.SendPrompt(t.ctx, schema.SendPromptRequest{
		UserID: t.userID,
		TabID:  tabID,
		Prompt: prompt,
	})
	if err != nil {
		t.logTab(tabID).Warn("tui prompt send failed", "err", err)
	}
	return err
}

func (t *terminalSession) appendError(tabID schema.TabID, err error) {
	if tabID == "" {
		t.appendNotice(fmt.Sprintf("error: %v", err))
		return
	}
	_, _ = t.service.AppendOutput(t.ctx, schema.AppendOutputRequest{
		UserID: t.userID,
		TabID:  tabID,
		Lines:  []string{fmt.Sprintf("error: %v", err)},
	})
}

func (t *terminalSession) appendMessage(tabID schema.TabID, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	if tabID == "" {
		_, _ = t.service.AppendSystemOutput(t.ctx, schema.AppendSystemOutputRequest{
			UserID: t.userID,
			Lines:  []string{message},
		})
		return
	}
	_, _ = t.service.AppendOutput(t.ctx, schema.AppendOutputRequest{
		UserID: t.userID,
		TabID:  tabID,
		Lines:  []string{message},
	})
}

func (t *terminalSession) appendNotice(message string) {
	t.notice = message
}

func (t *terminalSession) viewHeight() int {
	if t.height <= 1 {
		return 0
	}
	width := t.width
	if width <= 0 {
		width = 80
	}
	prefix, input := t.inputDisplay()
	inputLines, _, _ := renderInputLines(prefix, input, t.editor.cursor, width)
	inputHeight := len(inputLines)
	if inputHeight < 1 {
		inputHeight = 1
	}
	view := t.height - 1 - inputHeight
	if view < 0 {
		view = 0
	}
	return view
}

func (t *terminalSession) render() {
	width := t.width
	height := t.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	lines := make([]string, 0, height)
	theme := themeForName(t.themeName)
	tabLine, windowStart := renderTabBar(t.tabs, t.activeTab, width, theme, t.tabWindowStart)
	t.tabWindowStart = windowStart
	lines = append(lines, tabLine)

	viewLines := t.buffer.Lines
	if t.activeTab == "" {
		if t.notice != "" {
			viewLines = []string{t.notice}
		} else if len(t.system.Lines) > 0 {
			viewLines = t.system.Lines
		} else {
			viewLines = []string{"no active tab; use /new <repo>"}
		}
	}

	prefix, input := t.inputDisplay()
	inputLines, cursorRow, cursorCol := renderInputLines(stylePromptPrefix(prefix, theme), input, t.editor.cursor, width)
	outputHeight := height - 1 - len(inputLines)
	if outputHeight < 0 {
		outputHeight = 0
	}

	atBottom := t.buffer.AtBottom
	if t.activeTab == "" {
		atBottom = t.system.AtBottom
	}
	lines = append(lines, renderViewport(viewLines, width, outputHeight, theme, atBottom)...)

	lines = append(lines, inputLines...)
	cursorRow = len(lines) - len(inputLines) + cursorRow
	if err := t.screen.Render(lines, cursorRow, cursorCol); err != nil {
		t.log().Warn("tui render failed", "err", err)
	}
}

func (t *terminalSession) inputDisplay() (string, string) {
	prefix := t.promptPrefix()
	input := t.editor.String()
	if t.codexauth != nil {
		prefix = t.codexauth.prompt()
	} else if t.chpasswd != nil {
		prefix = t.chpasswd.prompt()
		input = maskInput(input)
	} else if t.rotateSSH != nil {
		prefix = t.rotateSSH.prompt()
	}
	return prefix, input
}

func renderViewport(viewLines []string, width, height int, theme tuiTheme, atBottom bool) []string {
	if height <= 0 {
		return nil
	}
	rendered := make([]string, 0, height)
	if atBottom {
		var flattened []string
		for _, raw := range viewLines {
			flattened = append(flattened, renderLines(raw, width, theme)...)
		}
		if len(flattened) > height {
			flattened = flattened[len(flattened)-height:]
		}
		rendered = append(rendered, flattened...)
	} else {
		count := 0
		for _, raw := range viewLines {
			if count >= height {
				break
			}
			for _, line := range renderLines(raw, width, theme) {
				if count >= height {
					break
				}
				rendered = append(rendered, line)
				count++
			}
		}
	}
	for len(rendered) < height {
		rendered = append(rendered, "")
	}
	return rendered
}

func (t *terminalSession) promptPrefix() string {
	if (t.running || t.commandSpinner.Load()) && len(spinnerFrames) > 0 {
		return fmt.Sprintf("%c ", spinnerFrames[t.spinnerIdx])
	}
	if t.promptIdle == "" {
		return "> "
	}
	return t.promptIdle
}

func isStatusCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/status") {
		return false
	}
	if len(trimmed) == len("/status") {
		return true
	}
	next := trimmed[len("/status")]
	return next == ' ' || next == '\t'
}

func isNewCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/new") {
		return false
	}
	if len(trimmed) == len("/new") {
		return true
	}
	next := trimmed[len("/new")]
	return next == ' ' || next == '\t'
}

func isChpasswdCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/chpasswd") {
		return false
	}
	if len(trimmed) == len("/chpasswd") {
		return true
	}
	next := trimmed[len("/chpasswd")]
	return next == ' ' || next == '\t'
}

func isCodexAuthCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/codexauth") {
		return false
	}
	if len(trimmed) == len("/codexauth") {
		return true
	}
	next := trimmed[len("/codexauth")]
	return next == ' ' || next == '\t'
}

func isRotateSSHKeyCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "/rotatesshkey"
}

func stylePromptPrefix(prefix string, theme tuiTheme) string {
	if strings.HasPrefix(prefix, ">") {
		return ansiBold + ansiFgRGB(theme.PromptFG) + ">" + ansiReset + strings.TrimPrefix(prefix, ">")
	}
	if spinner := spinnerPrefix(prefix); spinner != "" {
		return ansiFgRGB(theme.SpinnerFG) + spinner + ansiReset + strings.TrimPrefix(prefix, spinner)
	}
	return prefix
}

func spinnerPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(prefix)
	if first == utf8.RuneError && size == 0 {
		return ""
	}
	for _, frame := range spinnerFrames {
		if first == frame {
			return string(first)
		}
	}
	return ""
}

func maskInput(value string) string {
	if value == "" {
		return ""
	}
	return strings.Repeat("*", utf8.RuneCountInString(value))
}

var spinnerFrames = []rune{'|', '/', '-', '\\'}

var commandSpinnerDelay = 500 * time.Millisecond

func renderInputLines(prefix, input string, cursor, width int) ([]string, int, int) {
	inputRunes := []rune(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(inputRunes) {
		cursor = len(inputRunes)
	}
	prefixWidth := visibleWidth(prefix)
	if width <= 0 {
		width = prefixWidth + len(inputRunes) + 1
	}
	prefixVisible := prefix
	if prefixWidth > width {
		prefixVisible = trimANSIToWidth(prefix, width)
		prefixWidth = visibleWidth(prefixVisible)
	}
	indentWidth := prefixWidth
	indent := strings.Repeat(" ", indentWidth)
	availableFirst := width - prefixWidth
	if availableFirst < 1 {
		availableFirst = 1
	}
	availableOther := width - indentWidth
	if availableOther < 1 {
		availableOther = 1
	}

	lines := []string{}
	lineRunes := make([]rune, 0, availableFirst)
	row := 0
	col := 0
	cursorRow := 1
	cursorCol := prefixWidth + 1
	cursorSet := false
	currentAvailable := availableFirst

	flushLine := func() {
		prefixStr := prefixVisible
		if row > 0 {
			prefixStr = indent
		}
		lines = append(lines, prefixStr+string(lineRunes))
		row++
		lineRunes = lineRunes[:0]
		col = 0
		currentAvailable = availableOther
	}

	for i, r := range inputRunes {
		if !cursorSet && i == cursor {
			pfx := prefixWidth
			if row > 0 {
				pfx = indentWidth
			}
			cursorRow = row + 1
			cursorCol = pfx + col + 1
			cursorSet = true
		}
		if r == '\n' {
			flushLine()
			continue
		}
		if col >= currentAvailable {
			flushLine()
		}
		lineRunes = append(lineRunes, r)
		col++
	}
	if !cursorSet && cursor == len(inputRunes) {
		pfx := prefixWidth
		if row > 0 {
			pfx = indentWidth
		}
		cursorRow = row + 1
		cursorCol = pfx + col + 1
	}
	flushLine()
	if cursorCol < 1 {
		cursorCol = 1
	}
	if cursorCol > width {
		cursorCol = width
	}
	return lines, cursorRow, cursorCol
}

func trimToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}

func truncateName(name string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(name)
	if len(runes) <= max {
		return name
	}
	if max == 1 {
		return "$"
	}
	return string(append(runes[:max-1], '$'))
}

func tabsEqual(a, b []schema.TabSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tabStatusEqual(a, b map[schema.TabID]schema.TabStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for key, val := range a {
		if b[key] != val {
			return false
		}
	}
	return true
}

func bufferEqual(a, b schema.BufferSnapshot) bool {
	if a.TabID != b.TabID || a.TotalLines != b.TotalLines || a.ScrollOffset != b.ScrollOffset || a.AtBottom != b.AtBottom {
		return false
	}
	if len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	return true
}

func systemBufferEqual(a, b schema.SystemBufferSnapshot) bool {
	if a.TotalLines != b.TotalLines || a.ScrollOffset != b.ScrollOffset || a.AtBottom != b.AtBottom {
		return false
	}
	if len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	return true
}
