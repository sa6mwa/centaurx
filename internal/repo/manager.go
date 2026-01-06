package repo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// Manager handles repo discovery and creation under a fixed root.
type Manager struct {
	root string
	log  pslog.Logger
}

// NewManager ensures the root exists and returns a Manager.
func NewManager(root string) (*Manager, error) {
	return NewManagerWithLogger(root, nil)
}

// NewManagerWithLogger ensures the root exists and returns a Manager with logging.
func NewManagerWithLogger(root string, logger pslog.Logger) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, schema.ErrInvalidRepo
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if logger != nil {
		logger = logger.With("repo_root", root)
	}
	return &Manager{root: root, log: logger}, nil
}

// Root returns the repository root.
func (m *Manager) Root() string {
	return m.root
}

// CreateRepo creates a repo directory and runs git init.
func (m *Manager) CreateRepo(name schema.RepoName) (schema.RepoRef, error) {
	if m.log != nil {
		m.log.Info("repo manager create start", "repo", name)
	}
	repoName, err := normalizeRepoName(name)
	if err != nil {
		if m.log != nil {
			m.log.Warn("repo manager create failed", "err", err)
		}
		return schema.RepoRef{}, err
	}
	path := filepath.Join(m.root, string(repoName))
	if _, err := os.Stat(path); err == nil {
		if m.log != nil {
			m.log.Warn("repo manager create failed", "err", schema.ErrRepoExists)
		}
		return schema.RepoRef{}, schema.ErrRepoExists
	} else if !errors.Is(err, os.ErrNotExist) {
		if m.log != nil {
			m.log.Warn("repo manager create failed", "err", err)
		}
		return schema.RepoRef{}, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		if m.log != nil {
			m.log.Warn("repo manager create failed", "err", err)
		}
		return schema.RepoRef{}, err
	}
	if err := runGit(m.log, path, "init"); err != nil {
		return schema.RepoRef{}, err
	}
	if err := runGit(m.log, path, "switch", "-c", "centaurx"); err != nil {
		if err := runGit(m.log, path, "checkout", "-b", "centaurx"); err != nil {
			return schema.RepoRef{}, err
		}
	}
	if m.log != nil {
		m.log.Info("repo manager create ok", "path", path)
	}
	return schema.RepoRef{Name: repoName, Path: path}, nil
}

// ResolveRepo verifies the repo exists and has a .git directory.
func (m *Manager) ResolveRepo(name schema.RepoName) (schema.RepoRef, error) {
	if m.log != nil {
		m.log.Info("repo manager resolve start", "repo", name)
	}
	repoName, err := normalizeRepoName(name)
	if err != nil {
		if m.log != nil {
			m.log.Warn("repo manager resolve failed", "err", err)
		}
		return schema.RepoRef{}, err
	}
	path := filepath.Join(m.root, string(repoName))
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if m.log != nil {
				m.log.Warn("repo manager resolve failed", "err", schema.ErrRepoNotFound)
			}
			return schema.RepoRef{}, schema.ErrRepoNotFound
		}
		if m.log != nil {
			m.log.Warn("repo manager resolve failed", "err", err)
		}
		return schema.RepoRef{}, err
	}
	if !info.IsDir() {
		if m.log != nil {
			m.log.Warn("repo manager resolve failed", "err", schema.ErrRepoNotFound)
		}
		return schema.RepoRef{}, schema.ErrRepoNotFound
	}
	if !hasGitDir(path) {
		if m.log != nil {
			m.log.Warn("repo manager resolve failed", "err", schema.ErrRepoNotFound)
		}
		return schema.RepoRef{}, schema.ErrRepoNotFound
	}
	if m.log != nil {
		m.log.Debug("repo manager resolve ok", "path", path)
	}
	return schema.RepoRef{Name: repoName, Path: path}, nil
}

// ListRepos lists repos under root that contain a .git directory.
func (m *Manager) ListRepos() ([]schema.RepoRef, error) {
	if m.log != nil {
		m.log.Trace("repo manager list start")
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if m.log != nil {
			m.log.Warn("repo manager list failed", "err", err)
		}
		return nil, err
	}
	repos := make([]schema.RepoRef, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(m.root, name)
		if !hasGitDir(path) {
			continue
		}
		repos = append(repos, schema.RepoRef{Name: schema.RepoName(name), Path: path})
	}
	if m.log != nil {
		m.log.Debug("repo manager list ok", "count", len(repos))
	}
	return repos, nil
}

// OpenOrCloneURL resolves a repo name from a git URL and clones if needed.
func (m *Manager) OpenOrCloneURL(rawURL string) (schema.RepoRef, bool, error) {
	if m.log != nil {
		m.log.Info("repo manager clone start", "url", rawURL)
	}
	cloneURL, repoName, err := NormalizeGitURL(rawURL)
	if err != nil {
		if m.log != nil {
			m.log.Warn("repo manager clone failed", "err", err)
		}
		return schema.RepoRef{}, false, err
	}
	if repoRef, err := m.ResolveRepo(repoName); err == nil {
		if m.log != nil {
			m.log.Info("repo manager clone skipped", "repo", repoRef.Name)
		}
		return repoRef, false, nil
	} else if !errors.Is(err, schema.ErrRepoNotFound) {
		if m.log != nil {
			m.log.Warn("repo manager clone failed", "err", err)
		}
		return schema.RepoRef{}, false, err
	}

	path := filepath.Join(m.root, string(repoName))
	if _, err := os.Stat(path); err == nil {
		if m.log != nil {
			m.log.Warn("repo manager clone failed", "err", schema.ErrRepoExists)
		}
		return schema.RepoRef{}, false, schema.ErrRepoExists
	} else if !errors.Is(err, os.ErrNotExist) {
		if m.log != nil {
			m.log.Warn("repo manager clone failed", "err", err)
		}
		return schema.RepoRef{}, false, err
	}
	if err := runGit(m.log, m.root, "clone", cloneURL, path); err != nil {
		return schema.RepoRef{}, false, err
	}
	if m.log != nil {
		m.log.Info("repo manager clone ok", "repo", repoName, "path", path)
	}
	return schema.RepoRef{Name: repoName, Path: path}, true, nil
}

func normalizeRepoName(name schema.RepoName) (schema.RepoName, error) {
	trimmed := strings.TrimSpace(string(name))
	if trimmed == "" {
		return "", schema.ErrInvalidRepo
	}
	if strings.Contains(trimmed, string(filepath.Separator)) {
		return "", schema.ErrInvalidRepo
	}
	clean := filepath.Clean(trimmed)
	if clean == "." || clean == ".." || strings.Contains(clean, string(filepath.Separator)) {
		return "", schema.ErrInvalidRepo
	}
	return schema.RepoName(clean), nil
}

func hasGitDir(repoPath string) bool {
	stat, err := os.Stat(filepath.Join(repoPath, ".git"))
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func runGit(log pslog.Logger, dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if log != nil {
			log.Warn("repo manager git failed", "err", err, "command", strings.Join(args, " "), "output", strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("git %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	if log != nil {
		log.Trace("repo manager git ok", "command", strings.Join(args, " "))
	}
	return nil
}
