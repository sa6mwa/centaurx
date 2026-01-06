package core

import (
	"errors"
	"path/filepath"
	"strings"

	"pkt.systems/centaurx/internal/repo"
	"pkt.systems/centaurx/schema"
)

// MapRepoPath maps a host repo path to a runner repo root.
func MapRepoPath(hostRoot, runnerRoot, hostPath string) (string, error) {
	if strings.TrimSpace(runnerRoot) == "" || runnerRoot == hostRoot {
		return hostPath, nil
	}
	rel, err := filepath.Rel(hostRoot, hostPath)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return runnerRoot, nil
	}
	if strings.HasPrefix(rel, "..") {
		return "", schema.ErrInvalidRepo
	}
	return filepath.Join(runnerRoot, rel), nil
}

// RepoPath builds a repo path using the configured root and user/repo identity.
func RepoPath(repoRoot string, userID schema.UserID, repoName schema.RepoName) (string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", errors.New("repo root is required")
	}
	if strings.TrimSpace(string(userID)) == "" {
		return "", errors.New("user id is required")
	}
	normalized, err := repo.NormalizeRepoName(repoName)
	if err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, string(userID), string(normalized)), nil
}

// RepoRefForUser returns a repo ref with a computed path when possible.
func RepoRefForUser(repoRoot string, userID schema.UserID, repoName schema.RepoName) schema.RepoRef {
	path, err := RepoPath(repoRoot, userID, repoName)
	if err != nil {
		return schema.RepoRef{Name: repoName}
	}
	return schema.RepoRef{Name: repoName, Path: path}
}
