package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/internal/sessionprefs"
	"pkt.systems/centaurx/schema"
)

type session struct {
	id        string
	userID    schema.UserID
	expiresAt time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	prefs     *sessionprefs.Prefs
}

type sessionStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	baseCtx context.Context
	items   map[string]session
	path    string
}

func newSessionStore(ttl time.Duration, path string) *sessionStore {
	store := &sessionStore{
		ttl:     ttl,
		baseCtx: context.TODO(),
		items:   make(map[string]session),
		path:    strings.TrimSpace(path),
	}
	if store.path != "" {
		if err := store.load(); err != nil {
			logx.Ctx(context.Background()).Warn("session store load failed", "err", err)
		}
	}
	return store
}

func (s *sessionStore) create(userID schema.UserID) (string, session) {
	token := randomToken(32)
	entry := s.newSession(userID, time.Now().Add(s.ttl), "")
	log := logx.WithUser(context.Background(), userID).With("http_session", entry.id)
	s.mu.Lock()
	s.items[token] = entry
	s.mu.Unlock()
	s.persist()
	log.Info("session created", "expires", entry.expiresAt.Format(time.RFC3339))
	return token, entry
}

func (s *sessionStore) get(token string) (session, bool) {
	var expired bool
	s.mu.Lock()
	entry, ok := s.items[token]
	if !ok {
		s.mu.Unlock()
		return session{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.items, token)
		if entry.cancel != nil {
			entry.cancel()
		}
		logx.WithUser(context.Background(), entry.userID).With("http_session", entry.id).Info("session expired")
		expired = true
		s.mu.Unlock()
		if expired {
			s.persist()
		}
		return session{}, false
	}
	s.mu.Unlock()
	return entry, true
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	entry, ok := s.items[token]
	if ok {
		delete(s.items, token)
	}
	s.mu.Unlock()
	if ok && entry.cancel != nil {
		entry.cancel()
	}
	if ok {
		logx.WithUser(context.Background(), entry.userID).With("http_session", entry.id).Info("session deleted")
		s.persist()
	}
}

func (s *sessionStore) setBaseContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	s.mu.Lock()
	s.baseCtx = ctx
	for token, entry := range s.items {
		if entry.cancel != nil {
			entry.cancel()
		}
		prefs := entry.prefs
		if prefs == nil {
			prefs = sessionprefs.New()
		}
		parent := sessionprefs.WithContext(ctx, prefs)
		nextCtx, cancel := context.WithCancel(parent)
		entry.ctx = nextCtx
		entry.cancel = cancel
		entry.prefs = prefs
		s.items[token] = entry
	}
	s.mu.Unlock()
	logx.Ctx(context.Background()).Debug("session base context set")
}

func (s *sessionStore) baseContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseCtx != nil {
		return s.baseCtx
	}
	return context.TODO()
}

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

type sessionRecord struct {
	Token     string    `json:"token"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type sessionFile struct {
	Version  int             `json:"version"`
	Sessions []sessionRecord `json:"sessions"`
}

func (s *sessionStore) newSession(userID schema.UserID, expiresAt time.Time, sessionID string) session {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = randomToken(12)
	}
	parent := s.baseContext()
	prefs := sessionprefs.New()
	parent = sessionprefs.WithContext(parent, prefs)
	ctx, cancel := context.WithCancel(parent)
	return session{
		id:        sessionID,
		userID:    userID,
		expiresAt: expiresAt,
		ctx:       ctx,
		cancel:    cancel,
		prefs:     prefs,
	}
}

func (s *sessionStore) load() error {
	path := s.path
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	now := time.Now()
	entries := make(map[string]session)
	for _, record := range file.Sessions {
		if strings.TrimSpace(record.Token) == "" {
			continue
		}
		if strings.TrimSpace(record.UserID) == "" {
			continue
		}
		if now.After(record.ExpiresAt) {
			continue
		}
		entry := s.newSession(schema.UserID(record.UserID), record.ExpiresAt, record.SessionID)
		entries[record.Token] = entry
	}
	s.mu.Lock()
	s.items = entries
	s.mu.Unlock()
	if len(file.Sessions) != len(entries) {
		s.persist()
	}
	logx.Ctx(context.Background()).Info("session store loaded", "sessions", len(entries))
	return nil
}

func (s *sessionStore) persist() {
	if s.path == "" {
		return
	}
	records := s.snapshot()
	if err := writeSessionFile(s.path, records); err != nil {
		logx.Ctx(context.Background()).Warn("session store save failed", "err", err)
	}
}

func (s *sessionStore) snapshot() []sessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]sessionRecord, 0, len(s.items))
	for token, entry := range s.items {
		records = append(records, sessionRecord{
			Token:     token,
			SessionID: entry.id,
			UserID:    string(entry.userID),
			ExpiresAt: entry.expiresAt,
		})
	}
	return records
}

func writeSessionFile(path string, records []sessionRecord) error {
	payload := sessionFile{Version: 1, Sessions: records}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "sessions-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
