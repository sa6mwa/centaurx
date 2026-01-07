package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// User represents a stored user account.
type User struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	TOTPSecret   string   `json:"totp_secret"`
	LoginPubKeys []string `json:"login_pubkeys,omitempty"`
}

// Store manages users stored on disk.
type Store struct {
	path      string
	mu        sync.RWMutex
	users     map[string]User
	fileState fileState
	log       pslog.Logger
}

// NewStore loads or seeds the user store.
func NewStore(path string, seeds []appconfig.SeedUser) (*Store, error) {
	return NewStoreWithLogger(path, seeds, nil)
}

// NewStoreWithLogger loads or seeds the user store with logging.
func NewStoreWithLogger(path string, seeds []appconfig.SeedUser, logger pslog.Logger) (*Store, error) {
	if path == "" {
		return nil, errors.New("user file path is required")
	}
	if logger != nil {
		logger = logger.With("user_file", path)
	}
	store := &Store{
		path:  path,
		users: make(map[string]User),
		log:   logger,
	}
	if err := store.ensureFile(seeds); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Authenticate verifies username, password, and totp.
func (s *Store) Authenticate(username, password, totpCode string) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	s.mu.RLock()
	user, ok := s.users[username]
	s.mu.RUnlock()
	if !ok {
		return errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return errors.New("invalid credentials")
	}
	if !totp.Validate(totpCode, user.TOTPSecret) {
		return errors.New("invalid totp")
	}
	return nil
}

// ChangePassword verifies credentials and replaces the stored password hash.
func (s *Store) ChangePassword(username, currentPassword, totpCode, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new password is required")
	}
	if err := s.Authenticate(username, currentPassword, totpCode); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.UpdatePassword(username, string(hash))
}

// ValidateTOTP verifies the stored TOTP secret for a user.
func (s *Store) ValidateTOTP(username string, totpCode string) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	normalized, err := validateUsername(username)
	if err != nil {
		return err
	}
	s.mu.RLock()
	user, ok := s.users[normalized]
	s.mu.RUnlock()
	if !ok {
		return errors.New("invalid credentials")
	}
	if !totp.Validate(totpCode, user.TOTPSecret) {
		return errors.New("invalid totp")
	}
	return nil
}

// AddLoginPubKey adds a login public key for a user and returns its 1-based index.
func (s *Store) AddLoginPubKey(userID schema.UserID, pubKey string) (int, error) {
	if err := s.refreshIfNeeded(); err != nil {
		return 0, err
	}
	username, err := validateUsername(string(userID))
	if err != nil {
		return 0, err
	}
	normalized, parsed, err := normalizeLoginPubKey(pubKey)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[username]
	if !ok {
		return 0, errors.New("user not found")
	}
	for idx, existing := range user.LoginPubKeys {
		if keyEqual(existing, parsed) {
			return idx + 1, errors.New("login pubkey already exists")
		}
	}
	user.LoginPubKeys = append(user.LoginPubKeys, normalized)
	s.users[username] = user
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth pubkey add failed", "user", username, "err", err)
		}
		return 0, err
	}
	if s.log != nil {
		s.log.Info("auth pubkey added", "user", username, "id", len(user.LoginPubKeys))
	}
	return len(user.LoginPubKeys), nil
}

// ListLoginPubKeys returns the user's login public keys.
func (s *Store) ListLoginPubKeys(userID schema.UserID) ([]string, error) {
	if err := s.refreshIfNeeded(); err != nil {
		return nil, err
	}
	username, err := validateUsername(string(userID))
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	user, ok := s.users[username]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("user not found")
	}
	return append([]string{}, user.LoginPubKeys...), nil
}

// RemoveLoginPubKey removes the login public key at the provided 1-based index.
func (s *Store) RemoveLoginPubKey(userID schema.UserID, index int) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	username, err := validateUsername(string(userID))
	if err != nil {
		return err
	}
	if index <= 0 {
		return errors.New("login pubkey id must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	if index > len(user.LoginPubKeys) {
		return errors.New("login pubkey id out of range")
	}
	user.LoginPubKeys = append(user.LoginPubKeys[:index-1], user.LoginPubKeys[index:]...)
	s.users[username] = user
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth pubkey remove failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth pubkey removed", "user", username, "id", index)
	}
	return nil
}

// HasLoginPubKey reports whether the provided key is authorized for the user.
func (s *Store) HasLoginPubKey(userID schema.UserID, key ssh.PublicKey) (bool, error) {
	if err := s.refreshIfNeeded(); err != nil {
		return false, err
	}
	username, err := validateUsername(string(userID))
	if err != nil {
		return false, err
	}
	s.mu.RLock()
	user, ok := s.users[username]
	s.mu.RUnlock()
	if !ok {
		return false, errors.New("user not found")
	}
	for _, raw := range user.LoginPubKeys {
		if keyEqual(raw, key) {
			return true, nil
		}
	}
	return false, nil
}

// LoadUsers returns a snapshot of users.
func (s *Store) LoadUsers() []User {
	if err := s.refreshIfNeeded(); err != nil {
		if s.log != nil {
			s.log.Warn("auth store refresh failed", "err", err)
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users
}

// AddUser inserts a new user and persists the store.
func (s *Store) AddUser(user User) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	username, err := validateUsername(user.Username)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; ok {
		return errors.New("user already exists")
	}
	user.Username = username
	s.users[username] = user
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth user add failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth user added", "user", username)
	}
	return nil
}

// UpdatePassword replaces the stored password hash.
func (s *Store) UpdatePassword(username, passwordHash string) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	normalized, err := validateUsername(username)
	if err != nil {
		return err
	}
	username = normalized
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("password hash is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	user.PasswordHash = passwordHash
	s.users[username] = user
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth password update failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth password updated", "user", username)
	}
	return nil
}

// UpdateTOTP replaces the stored TOTP secret.
func (s *Store) UpdateTOTP(username, secret string) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	normalized, err := validateUsername(username)
	if err != nil {
		return err
	}
	username = normalized
	if strings.TrimSpace(secret) == "" {
		return errors.New("totp secret is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[username]
	if !ok {
		return errors.New("user not found")
	}
	user.TOTPSecret = secret
	s.users[username] = user
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth totp update failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth totp updated", "user", username)
	}
	return nil
}

// DeleteUser removes a user.
func (s *Store) DeleteUser(username string) error {
	if err := s.refreshIfNeeded(); err != nil {
		return err
	}
	normalized, err := validateUsername(username)
	if err != nil {
		return err
	}
	username = normalized
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; !ok {
		return errors.New("user not found")
	}
	delete(s.users, username)
	if err := s.saveLocked(); err != nil {
		if s.log != nil {
			s.log.Warn("auth user delete failed", "user", username, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth user deleted", "user", username)
	}
	return nil
}

func (s *Store) ensureFile(seeds []appconfig.SeedUser) error {
	if _, statErr := os.Stat(s.path); statErr == nil {
		return nil
	} else if !os.IsNotExist(statErr) {
		if s.log != nil {
			s.log.Warn("auth store init failed", "err", statErr)
		}
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		if s.log != nil {
			s.log.Warn("auth store init failed", "err", err)
		}
		return err
	}
	users := make([]User, 0, len(seeds))
	for _, seed := range seeds {
		if _, err := validateUsername(seed.Username); err != nil {
			return err
		}
		users = append(users, User{
			Username:     seed.Username,
			PasswordHash: seed.PasswordHash,
			TOTPSecret:   seed.TOTPSecret,
		})
	}
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store init failed", "err", err)
		}
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		if s.log != nil {
			s.log.Warn("auth store init failed", "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("auth store initialized", "users", len(users))
	}
	return nil
}

func (s *Store) load() error {
	return s.loadFromDisk()
}

func validateUsername(username string) (string, error) {
	if err := schema.ValidateUserID(schema.UserID(username)); err != nil {
		return "", errors.New("invalid username")
	}
	return username, nil
}

func (s *Store) saveLocked() error {
	users := make([]User, 0, len(s.users))
	keys := make([]string, 0, len(s.users))
	for key := range s.users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		users = append(users, s.users[key])
	}
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "users-*.json")
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		if s.log != nil {
			s.log.Warn("auth store save failed", "err", err)
		}
		return err
	}
	if info, err := os.Stat(s.path); err == nil {
		s.fileState = fileStateFromInfo(info)
	} else if s.log != nil {
		s.log.Warn("auth store save failed to stat", "err", err)
	}
	if s.log != nil {
		s.log.Debug("auth store save ok", "users", len(users))
	}
	return nil
}

type fileState struct {
	modTime time.Time
	size    int64
	inode   uint64
	dev     uint64
}

func fileStateFromInfo(info os.FileInfo) fileState {
	state := fileState{
		modTime: info.ModTime(),
		size:    info.Size(),
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		state.inode = stat.Ino
		state.dev = stat.Dev
	}
	return state
}

func (s fileState) equal(other fileState) bool {
	return s.size == other.size &&
		s.modTime.Equal(other.modTime) &&
		s.inode == other.inode &&
		s.dev == other.dev
}

func (s *Store) refreshIfNeeded() error {
	info, err := os.Stat(s.path)
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store stat failed", "err", err)
		}
		return err
	}
	latest := fileStateFromInfo(info)
	s.mu.RLock()
	current := s.fileState
	s.mu.RUnlock()
	if current.equal(latest) {
		return nil
	}
	return s.loadFromDisk()
}

func (s *Store) loadFromDisk() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store load failed", "err", err)
		}
		return err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		if s.log != nil {
			s.log.Warn("auth store load failed", "err", err)
		}
		return err
	}
	info, err := os.Stat(s.path)
	if err != nil {
		if s.log != nil {
			s.log.Warn("auth store load failed", "err", err)
		}
		return err
	}
	next := make(map[string]User, len(users))
	for _, user := range users {
		if _, err := validateUsername(user.Username); err != nil {
			if s.log != nil {
				s.log.Warn("auth store load failed", "err", err)
			}
			return err
		}
		next[user.Username] = user
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = next
	s.fileState = fileStateFromInfo(info)
	if s.log != nil {
		s.log.Debug("auth store load ok", "users", len(users))
	}
	return nil
}

func normalizeLoginPubKey(raw string) (string, ssh.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, errors.New("pubkey is required")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil {
		return "", nil, errors.New("invalid pubkey")
	}
	return trimmed, key, nil
}

func keyEqual(raw string, key ssh.PublicKey) bool {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(raw)))
	if err != nil {
		return false
	}
	return bytes.Equal(parsed.Marshal(), key.Marshal())
}
