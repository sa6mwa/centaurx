package schema

import (
	"errors"
	"os"
	"path/filepath"
)

// ServiceConfig defines defaults and limits for the core service.
type ServiceConfig struct {
	RepoRoot       string
	StateDir       string
	DefaultModel   ModelID
	AllowedModels  []ModelID
	DefaultTheme   ThemeName
	TabNameMax     int
	TabNameSuffix  string
	BufferMaxLines int
	// DisableAuditLogging disables audit trail debug logs for commands.
	DisableAuditLogging bool
}

// DefaultBufferMaxLines is the default per-tab buffer limit.
const DefaultBufferMaxLines = 5000

// NormalizeServiceConfig applies defaults and validates the config.
func NormalizeServiceConfig(cfg ServiceConfig) (ServiceConfig, error) {
	if cfg.RepoRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceConfig{}, err
		}
		cfg.RepoRoot = filepath.Join(home, ".centaurx", "repos")
	}
	if cfg.StateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceConfig{}, err
		}
		cfg.StateDir = filepath.Join(home, ".centaurx", "state")
	}
	if cfg.TabNameMax <= 0 {
		cfg.TabNameMax = 10
	}
	if cfg.TabNameSuffix == "" {
		cfg.TabNameSuffix = "$"
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = ModelID("gpt-5.2-codex")
	}
	if len(cfg.AllowedModels) == 0 {
		cfg.AllowedModels = []ModelID{
			"gpt-5.2-codex",
			"gpt-5.1-codex-max",
			"gpt-5.1-codex-mini",
		}
	}
	if cfg.DefaultTheme == "" {
		cfg.DefaultTheme = DefaultTheme
	}
	if cfg.BufferMaxLines <= 0 {
		cfg.BufferMaxLines = DefaultBufferMaxLines
	}
	if cfg.TabNameMax <= len(cfg.TabNameSuffix) {
		return ServiceConfig{}, errors.New("tab name max must exceed suffix length")
	}
	return cfg, nil
}
