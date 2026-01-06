package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{
		ttl:     ttl,
		baseCtx: context.TODO(),
		items:   make(map[string]session),
	}
}

func (s *sessionStore) create(userID schema.UserID) (string, session) {
	token := randomToken(32)
	sessionID := randomToken(12)
	log := logx.WithUser(context.Background(), userID).With("http_session", sessionID)
	parent := s.baseContext()
	prefs := sessionprefs.New()
	parent = sessionprefs.WithContext(parent, prefs)
	ctx, cancel := context.WithCancel(parent)
	entry := session{
		id:        sessionID,
		userID:    userID,
		expiresAt: time.Now().Add(s.ttl),
		ctx:       ctx,
		cancel:    cancel,
		prefs:     prefs,
	}
	s.mu.Lock()
	s.items[token] = entry
	s.mu.Unlock()
	log.Info("session created", "expires", entry.expiresAt.Format(time.RFC3339))
	return token, entry
}

func (s *sessionStore) get(token string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[token]
	if !ok {
		return session{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.items, token)
		if entry.cancel != nil {
			entry.cancel()
		}
		logx.WithUser(context.Background(), entry.userID).With("http_session", entry.id).Info("session expired")
		return session{}, false
	}
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
	}
}

func (s *sessionStore) setBaseContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	s.mu.Lock()
	s.baseCtx = ctx
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
