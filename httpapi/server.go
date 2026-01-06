package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// Authenticator verifies username, password, and totp.
type Authenticator interface {
	Authenticate(username, password, totp string) error
	ChangePassword(username, currentPassword, totp, newPassword string) error
}

// CommandHandler routes slash commands.
type CommandHandler interface {
	Handle(ctx context.Context, userID schema.UserID, tabID schema.TabID, input string) (bool, error)
}

// Server serves the HTTP API and UI.
type Server struct {
	cfg        Config
	service    core.Service
	cmdHandler CommandHandler
	authStore  Authenticator
	sessions   *sessionStore
	hub        *Hub
	basePath   string
	baseHref   string
}

// NewServer constructs an HTTP server.
func NewServer(cfg Config, service core.Service, handler CommandHandler, authStore Authenticator, hub *Hub) *Server {
	ttl := time.Duration(cfg.SessionTTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 720 * time.Hour
	}
	return &Server{
		cfg:        cfg,
		service:    service,
		cmdHandler: handler,
		authStore:  authStore,
		sessions:   newSessionStore(ttl),
		hub:        hub,
		basePath:   normalizeBasePath(cfg.BasePath),
		baseHref:   buildBaseHref(cfg.BaseURL, cfg.BasePath),
	}
}

// SetBaseContext sets the parent context for session lifetimes.
func (s *Server) SetBaseContext(ctx context.Context) {
	if s == nil || ctx == nil {
		return
	}
	s.sessions.setBaseContext(ctx)
}

// Handler returns an http.Handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))

	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/chpasswd", s.requireSession(s.handleChangePassword))
	mux.HandleFunc("/api/codexauth", s.requireSession(s.handleCodexAuth))
	mux.HandleFunc("/api/me", s.requireSession(s.handleMe))
	mux.HandleFunc("/api/tabs", s.requireSession(s.handleTabs))
	mux.HandleFunc("/api/tabs/activate", s.requireSession(s.handleActivate))
	mux.HandleFunc("/api/prompt", s.requireSession(s.handlePrompt))
	mux.HandleFunc("/api/buffer", s.requireSession(s.handleBuffer))
	mux.HandleFunc("/api/system", s.requireSession(s.handleSystemBuffer))
	mux.HandleFunc("/api/history", s.requireSession(s.handleHistory))
	mux.HandleFunc("/api/stream", s.requireSession(s.handleStream))

	handler := withRequestLogging(mux, s.lookupSession)
	if s.basePath == "" {
		return handler
	}
	prefix := s.basePath
	root := http.NewServeMux()
	root.Handle(prefix+"/", http.StripPrefix(prefix, handler))
	root.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, prefix+"/", http.StatusTemporaryRedirect)
	})
	return root
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(assetsFS, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	stat, err := fs.Stat(assetsFS, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	data = applyBaseHref(data, s.baseHref)
	data = applyUIMaxBufferLines(data, s.cfg.UIMaxBufferLines)
	reader := bytes.NewReader(data)
	http.ServeContent(w, r, "index.html", stat.ModTime(), reader)
}

const baseHrefPlaceholder = "<!-- BASE_HREF -->"
const uiMaxBufferLinesPlaceholder = "UI_MAX_BUFFER_LINES"
const defaultUIMaxBufferLines = 2000

func applyBaseHref(data []byte, baseHref string) []byte {
	replacement := ""
	if strings.TrimSpace(baseHref) != "" {
		replacement = fmt.Sprintf(`<base href="%s" />`, html.EscapeString(baseHref))
	}
	return bytes.ReplaceAll(data, []byte(baseHrefPlaceholder), []byte(replacement))
}

func applyUIMaxBufferLines(data []byte, maxLines int) []byte {
	if maxLines <= 0 {
		maxLines = defaultUIMaxBufferLines
	}
	replacement := []byte(fmt.Sprintf("%d", maxLines))
	return bytes.ReplaceAll(data, []byte(uiMaxBufferLinesPlaceholder), replacement)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.Ctx(r.Context()).With("remote", clientIP(r))
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if err := decodeJSON(r.Body, &payload); err != nil {
		log.Warn("http login decode failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	log = log.With("user", payload.Username)
	if err := s.authStore.Authenticate(payload.Username, payload.Password, payload.TOTP); err != nil {
		log.Warn("http login failed", "err", err)
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	token, sess := s.sessions.create(schema.UserID(payload.Username))
	cookie := &http.Cookie{
		Name:     s.cfg.SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.expiresAt,
	}
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]any{"username": payload.Username})
	log.Info("http login ok")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := s.sessionToken(r)
	log := logx.Ctx(r.Context()).With("remote", clientIP(r))
	if token != "" {
		if entry, ok := s.sessions.get(token); ok {
			log = log.With("user", entry.userID, "http_session", entry.id)
		}
		s.sessions.delete(token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	log.Info("http logout")
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.Ctx(r.Context()).With("user", userID, "remote", clientIP(r))
	log.Info("http chpasswd request", "command", "/chpasswd")
	var payload struct {
		CurrentPassword string `json:"current_password"`
		TOTP            string `json:"totp"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := decodeJSON(r.Body, &payload); err != nil {
		log.Warn("http chpasswd decode failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(payload.CurrentPassword) == "" {
		writeError(w, http.StatusBadRequest, errors.New("current password is required"))
		return
	}
	if strings.TrimSpace(payload.NewPassword) == "" {
		writeError(w, http.StatusBadRequest, errors.New("new password is required"))
		return
	}
	if strings.TrimSpace(payload.ConfirmPassword) == "" {
		writeError(w, http.StatusBadRequest, errors.New("confirm password is required"))
		return
	}
	if payload.NewPassword != payload.ConfirmPassword {
		writeError(w, http.StatusBadRequest, errors.New("passwords do not match"))
		return
	}
	if strings.TrimSpace(payload.TOTP) == "" {
		writeError(w, http.StatusBadRequest, errors.New("totp is required"))
		return
	}
	if err := s.authStore.ChangePassword(string(userID), payload.CurrentPassword, payload.TOTP, payload.NewPassword); err != nil {
		log.Warn("http chpasswd failed", "err", err)
		log.Info("http chpasswd rejected", "command", "/chpasswd")
		status := http.StatusInternalServerError
		switch {
		case isPasswordChangeAuthError(err):
			status = http.StatusUnauthorized
		case isPasswordChangeValidationError(err):
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	ctx := sessionContext(r.Context())
	tabID := s.resolveTabID(ctx, userID, "")
	if tabID != "" {
		_, _ = s.service.AppendOutput(ctx, schema.AppendOutputRequest{
			UserID: userID,
			TabID:  tabID,
			Lines:  []string{"password updated"},
		})
	} else {
		_, _ = s.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
			UserID: userID,
			Lines:  []string{"password updated"},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	log.Info("http chpasswd ok", "command", "/chpasswd")
}

func (s *Server) handleCodexAuth(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.Ctx(r.Context()).With("user", userID, "remote", clientIP(r))
	log.Info("http codexauth request", "command", "/codexauth")
	const maxCodexAuthSize = 2 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCodexAuthSize+1))
	if err != nil {
		log.Warn("http codexauth read failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(body) > maxCodexAuthSize {
		writeError(w, http.StatusBadRequest, errors.New("auth.json exceeds 2MB limit"))
		return
	}
	if _, err := s.service.SaveCodexAuth(r.Context(), schema.SaveCodexAuthRequest{
		UserID:   userID,
		AuthJSON: body,
	}); err != nil {
		log.Warn("http codexauth failed", "err", err)
		status := http.StatusInternalServerError
		if isCodexAuthValidationError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	ctx := sessionContext(r.Context())
	tabID := s.resolveTabID(ctx, userID, "")
	if tabID != "" {
		_, _ = s.service.AppendOutput(ctx, schema.AppendOutputRequest{
			UserID: userID,
			TabID:  tabID,
			Lines:  []string{"codex auth updated"},
		})
	} else {
		_, _ = s.service.AppendSystemOutput(ctx, schema.AppendSystemOutputRequest{
			UserID: userID,
			Lines:  []string{"codex auth updated"},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	log.Info("http codexauth ok", "command", "/codexauth")
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	writeJSON(w, http.StatusOK, map[string]any{"username": userID})
}

func (s *Server) handleTabs(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	log := logx.WithUser(r.Context(), userID)
	ctx := sessionContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		resp, err := s.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
		if err != nil {
			log.Warn("http tabs list failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		log.Info("http tabs list ok", "count", len(resp.Tabs))
	case http.MethodPost:
		var payload struct {
			RepoName string `json:"repo_name"`
			Create   bool   `json:"create"`
		}
		if err := decodeJSON(r.Body, &payload); err != nil {
			log.Warn("http tabs decode failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp, err := s.service.CreateTab(ctx, schema.CreateTabRequest{
			UserID:     userID,
			RepoName:   schema.RepoName(payload.RepoName),
			CreateRepo: payload.Create,
		})
		if err != nil {
			log.Warn("http tabs create failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		log.Info("http tabs create ok", "tab", resp.Tab.ID, "repo", resp.Tab.Repo.Name)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.WithUser(r.Context(), userID)
	ctx := sessionContext(r.Context())
	var payload struct {
		TabID string `json:"tab_id"`
	}
	if err := decodeJSON(r.Body, &payload); err != nil {
		log.Warn("http activate decode failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp, err := s.service.ActivateTab(ctx, schema.ActivateTabRequest{
		UserID: userID,
		TabID:  schema.TabID(payload.TabID),
	})
	if err != nil {
		log.Warn("http activate failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
	log.Info("http activate ok", "tab", resp.Tab.ID)
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.WithUser(r.Context(), userID)
	var payload struct {
		TabID string `json:"tab_id"`
		Input string `json:"input"`
	}
	if err := decodeJSON(r.Body, &payload); err != nil {
		log.Warn("http prompt decode failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	log = log.With("tab", payload.TabID, "input_len", len(payload.Input))
	if isLogoutCommand(payload.Input) {
		s.handleLogout(w, r)
		return
	}
	ctx := sessionContext(r.Context())
	tabID := s.resolveTabID(ctx, userID, schema.TabID(payload.TabID))
	if tabID != "" && strings.TrimSpace(payload.Input) != "" {
		_, _ = s.service.AppendHistory(ctx, schema.AppendHistoryRequest{
			UserID: userID,
			TabID:  tabID,
			Entry:  payload.Input,
		})
	}
	handled, err := s.cmdHandler.Handle(ctx, userID, tabID, payload.Input)
	if err != nil {
		log.Warn("http prompt command failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if handled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		log.Info("http prompt command ok")
		return
	}
	if tabID == "" {
		writeError(w, http.StatusBadRequest, errors.New("no active tab; use /new <repo>"))
		log.Warn("http prompt rejected", "reason", "no active tab")
		return
	}
	resp, err := s.service.SendPrompt(ctx, schema.SendPromptRequest{
		UserID: userID,
		TabID:  tabID,
		Prompt: payload.Input,
	})
	if err != nil {
		if errors.Is(err, schema.ErrTabNotFound) {
			fallback := s.resolveTabID(ctx, userID, "")
			if fallback != "" && fallback != tabID {
				resp, err = s.service.SendPrompt(ctx, schema.SendPromptRequest{
					UserID: userID,
					TabID:  fallback,
					Prompt: payload.Input,
				})
			}
		}
		if err != nil {
			log.Warn("http prompt failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if err != nil {
		log.Warn("http prompt failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
	log.Info("http prompt ok")
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	ctx := sessionContext(r.Context())
	log := logx.WithUser(r.Context(), userID)
	switch r.Method {
	case http.MethodGet:
		tabID := schema.TabID(r.URL.Query().Get("tab_id"))
		if tabID == "" {
			tabID = s.resolveTabID(ctx, userID, "")
		}
		if tabID == "" {
			writeError(w, http.StatusBadRequest, errors.New("no active tab; use /new <repo>"))
			log.Warn("http history rejected", "reason", "no active tab")
			return
		}
		resp, err := s.service.GetHistory(ctx, schema.GetHistoryRequest{
			UserID: userID,
			TabID:  tabID,
		})
		if err != nil {
			log.Warn("http history get failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		log.Info("http history get ok", "tab", tabID, "entries", len(resp.Entries))
	case http.MethodPost:
		var payload struct {
			TabID string `json:"tab_id"`
			Entry string `json:"entry"`
		}
		if err := decodeJSON(r.Body, &payload); err != nil {
			log.Warn("http history decode failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		tabID := s.resolveTabID(ctx, userID, schema.TabID(payload.TabID))
		if tabID == "" {
			writeError(w, http.StatusBadRequest, errors.New("no active tab; use /new <repo>"))
			log.Warn("http history rejected", "reason", "no active tab")
			return
		}
		if strings.TrimSpace(payload.Entry) == "" {
			resp, err := s.service.GetHistory(ctx, schema.GetHistoryRequest{
				UserID: userID,
				TabID:  tabID,
			})
			if err != nil {
				log.Warn("http history get failed", "err", err)
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, resp)
			log.Info("http history get ok", "tab", tabID, "entries", len(resp.Entries))
			return
		}
		resp, err := s.service.AppendHistory(ctx, schema.AppendHistoryRequest{
			UserID: userID,
			TabID:  tabID,
			Entry:  payload.Entry,
		})
		if err != nil {
			log.Warn("http history append failed", "err", err)
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		log.Info("http history append ok", "tab", tabID, "entries", len(resp.Entries))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBuffer(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.WithUser(r.Context(), userID)
	tabID := schema.TabID(r.URL.Query().Get("tab_id"))
	limit := parseInt(r.URL.Query().Get("limit"), s.cfg.InitialBufferLines)
	resp, err := s.service.GetBuffer(r.Context(), schema.GetBufferRequest{
		UserID: userID,
		TabID:  tabID,
		Limit:  limit,
	})
	if err != nil {
		log.Warn("http buffer failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
	log.Debug("http buffer ok", "tab", tabID, "lines", resp.Buffer.TotalLines)
}

func (s *Server) handleSystemBuffer(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log := logx.WithUser(r.Context(), userID)
	limit := parseInt(r.URL.Query().Get("limit"), s.cfg.InitialBufferLines)
	resp, err := s.service.GetSystemBuffer(r.Context(), schema.GetSystemBufferRequest{
		UserID: userID,
		Limit:  limit,
	})
	if err != nil {
		log.Warn("http system buffer failed", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
	log.Debug("http system buffer ok", "lines", resp.Buffer.TotalLines)
}

func isLogoutCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	switch trimmed {
	case "/quit", "/exit", "/logout", "/q":
		return true
	default:
		return false
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, userID schema.UserID) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("stream unsupported"))
		return
	}
	log := logx.WithUser(r.Context(), userID)
	ctx := sessionContext(r.Context())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastID := parseUint(r.Header.Get("Last-Event-ID"))

	snapshot := s.buildSnapshot(ctx, userID)
	snapshotTabs := len(snapshot.Tabs)
	snapshotBuffers := len(snapshot.Buffers)
	_ = writeSSEvent(w, StreamEvent{
		Type:      "snapshot",
		Snapshot:  &snapshot,
		Timestamp: time.Now(),
	})
	flusher.Flush()

	replayCount := 0
	if lastID > 0 {
		replay := s.hub.Replay(userID, lastID)
		replayCount = len(replay)
		for _, event := range replay {
			_ = writeSSEvent(w, event)
		}
		flusher.Flush()
	}

	ch, unsubscribe, _, _ := s.hub.Subscribe(userID)
	defer unsubscribe()

	notify := r.Context().Done()
	log.Info("http stream opened", "last_id", lastID, "replay", replayCount, "tabs", snapshotTabs, "buffers", snapshotBuffers)
	for {
		select {
		case <-notify:
			log.Info("http stream closed")
			return
		case event := <-ch:
			_ = writeSSEvent(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) buildSnapshot(ctx context.Context, userID schema.UserID) SnapshotPayload {
	resp, err := s.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		return SnapshotPayload{}
	}
	buffers := make(map[schema.TabID]schema.BufferSnapshot)
	for _, tab := range resp.Tabs {
		bufferResp, err := s.service.GetBuffer(ctx, schema.GetBufferRequest{
			UserID: userID,
			TabID:  tab.ID,
			Limit:  s.cfg.InitialBufferLines,
		})
		if err != nil {
			continue
		}
		buffers[tab.ID] = bufferResp.Buffer
	}
	system := schema.SystemBufferSnapshot{}
	if sysResp, err := s.service.GetSystemBuffer(ctx, schema.GetSystemBufferRequest{
		UserID: userID,
		Limit:  s.cfg.InitialBufferLines,
	}); err == nil {
		system = sysResp.Buffer
	}
	return SnapshotPayload{
		Tabs:      resp.Tabs,
		ActiveTab: resp.ActiveTab,
		Buffers:   buffers,
		System:    system,
		Theme:     resp.Theme,
	}
}

func (s *Server) resolveTabID(ctx context.Context, userID schema.UserID, requested schema.TabID) schema.TabID {
	if userID == "" {
		return ""
	}
	resp, err := s.service.ListTabs(ctx, schema.ListTabsRequest{UserID: userID})
	if err != nil {
		return requested
	}
	if requested != "" {
		for _, tab := range resp.Tabs {
			if tab.ID == requested {
				return requested
			}
		}
	}
	if resp.ActiveTab != "" {
		return resp.ActiveTab
	}
	return requested
}

func (s *Server) requireSession(next func(http.ResponseWriter, *http.Request, schema.UserID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logx.Ctx(r.Context()).With("remote", clientIP(r))
		token := s.sessionToken(r)
		if token == "" {
			log.Warn("http session missing")
			writeError(w, http.StatusUnauthorized, errors.New("missing session"))
			return
		}
		entry, ok := s.sessions.get(token)
		if !ok {
			log.Warn("http session invalid")
			writeError(w, http.StatusUnauthorized, errors.New("invalid session"))
			return
		}
		log = log.With("user", entry.userID, "http_session", entry.id)
		ctx := logx.ContextWithUserLogger(r.Context(), log, entry.userID)
		ctx = withSessionContext(ctx, entry)
		next(w, r.WithContext(ctx), entry.userID)
	}
}

type sessionContextKey struct{}

func withSessionContext(ctx context.Context, sess session) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, sess)
}

func sessionContext(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(sessionContextKey{})
	sess, ok := value.(session)
	if !ok || sess.ctx == nil {
		return ctx
	}
	logger := pslog.Ctx(ctx)
	return logx.CopyContextFields(pslog.ContextWithLogger(sess.ctx, logger), ctx)
}

func (s *Server) sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(s.cfg.SessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) lookupSession(r *http.Request) (schema.UserID, string) {
	if s == nil || r == nil {
		return "", ""
	}
	token := s.sessionToken(r)
	if token == "" {
		return "", ""
	}
	entry, ok := s.sessions.get(token)
	if !ok {
		return "", ""
	}
	return entry.userID, entry.id
}

func decodeJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeSSEvent(w http.ResponseWriter, event StreamEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if event.Seq > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", event.Seq)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(string(data)))
	return nil
}

func parseUint(value string) uint64 {
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func isPasswordChangeAuthError(err error) bool {
	if err == nil {
		return false
	}
	switch strings.TrimSpace(err.Error()) {
	case "invalid credentials", "invalid totp", "user not found":
		return true
	default:
		return false
	}
}

func isPasswordChangeValidationError(err error) bool {
	if err == nil {
		return false
	}
	switch strings.TrimSpace(err.Error()) {
	case "current password is required", "totp is required", "new password is required", "confirm password is required", "passwords do not match":
		return true
	default:
		return false
	}
}

func isCodexAuthValidationError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, schema.ErrInvalidRequest) || errors.Is(err, schema.ErrInvalidCodexAuth)
}
