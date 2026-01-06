package core

import (
	"path/filepath"
	"strings"

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
