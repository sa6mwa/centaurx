package persist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// BufferSnapshot captures buffer state for persistence.
type BufferSnapshot struct {
	Lines        []string `json:"lines"`
	ScrollOffset int      `json:"scroll_offset"`
}

// TabSnapshot captures a tab for persistence.
type TabSnapshot struct {
	ID        schema.TabID     `json:"id"`
	Name      schema.TabName   `json:"name"`
	Repo      schema.RepoRef   `json:"repo"`
	Model     schema.ModelID   `json:"model"`
	SessionID schema.SessionID `json:"session_id"`
	Buffer    BufferSnapshot   `json:"buffer"`
	History   []string         `json:"history,omitempty"`
}

// UserSnapshot captures a user's tab state for persistence.
type UserSnapshot struct {
	Order  []schema.TabID   `json:"order"`
	Tabs   []TabSnapshot    `json:"tabs"`
	System BufferSnapshot   `json:"system,omitempty"`
	Theme  schema.ThemeName `json:"theme,omitempty"`
}

// Store persists user snapshots to disk.
type Store struct {
	dir string
	log pslog.Logger
}

// NewStore constructs a persistent store at the given directory.
func NewStore(dir string) (*Store, error) {
	return NewStoreWithLogger(dir, nil)
}

// NewStoreWithLogger constructs a persistent store with logging.
func NewStoreWithLogger(dir string, logger pslog.Logger) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if logger != nil {
		logger = logger.With("state_dir", dir)
	}
	return &Store{dir: dir, log: logger}, nil
}

// Load reads a user snapshot from disk.
func (s *Store) Load(userID schema.UserID) (UserSnapshot, bool, error) {
	path := s.pathForUser(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if s.log != nil {
				s.log.Debug("state load miss", "user", userID)
			}
			return UserSnapshot{}, false, nil
		}
		if s.log != nil {
			s.log.Warn("state load failed", "user", userID, "err", err)
		}
		return UserSnapshot{}, false, err
	}
	var snapshot UserSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		if s.log != nil {
			s.log.Warn("state load failed", "user", userID, "err", err)
		}
		return UserSnapshot{}, false, err
	}
	if s.log != nil {
		s.log.Debug("state load ok", "user", userID, "tabs", len(snapshot.Tabs))
	}
	return snapshot, true, nil
}

// Save writes a user snapshot to disk.
func (s *Store) Save(userID schema.UserID, snapshot UserSnapshot) error {
	path := s.pathForUser(userID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "state-*.json")
	if err != nil {
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if s.log != nil {
			s.log.Warn("state save failed", "user", userID, "err", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Trace("state save ok", "user", userID, "tabs", len(snapshot.Tabs))
	}
	return nil
}

func (s *Store) pathForUser(userID schema.UserID) string {
	name := sanitize(string(userID))
	if name == "" {
		name = "unknown"
	}
	return filepath.Join(s.dir, name+".json")
}

func sanitize(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		if r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	return b.String()
}
