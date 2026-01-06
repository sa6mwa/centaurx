package userhome

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"pkt.systems/centaurx/internal/appconfig"
)

// TemplateData supplies values for rendering skel templates.
type TemplateData struct {
	Config appconfig.Config
	Codex  CodexConfig
}

// CodexConfig captures default codex settings for config.toml templates.
type CodexConfig struct {
	Model                 string
	ModelReasoningEffort  string
	ApprovalPolicy        string
	SandboxMode           string
	Features              CodexFeatures
	SandboxWorkspaceWrite CodexSandboxWorkspaceWrite
}

// CodexFeatures exposes template flags for codex feature toggles.
type CodexFeatures struct {
	WebSearchRequest bool
}

// CodexSandboxWorkspaceWrite exposes template flags for sandbox policy.
type CodexSandboxWorkspaceWrite struct {
	NetworkAccess bool
}

const defaultCodexConfigTemplate = `model = "{{ .Codex.Model }}"
model_reasoning_effort = "{{ .Codex.ModelReasoningEffort }}"
approval_policy = "{{ .Codex.ApprovalPolicy }}"
sandbox_mode = "{{ .Codex.SandboxMode }}"

[features]
web_search_request = {{ .Codex.Features.WebSearchRequest }}

[sandbox_workspace_write]
network_access = {{ .Codex.SandboxWorkspaceWrite.NetworkAccess }}
`

// DefaultTemplateData returns template data with codex defaults based on app config.
func DefaultTemplateData(cfg appconfig.Config) TemplateData {
	return TemplateData{
		Config: cfg,
		Codex: CodexConfig{
			Model:                cfg.Models.Default,
			ModelReasoningEffort: "medium",
			ApprovalPolicy:       "on-request",
			SandboxMode:          "danger-full-access",
			Features: CodexFeatures{
				WebSearchRequest: true,
			},
			SandboxWorkspaceWrite: CodexSandboxWorkspaceWrite{
				NetworkAccess: true,
			},
		},
	}
}

// SkelDir returns the default skel path for the given state directory.
func SkelDir(stateDir string) string {
	base := filepath.Dir(stateDir)
	return filepath.Join(base, "files", "skel")
}

// HomeRoot returns the root directory for per-user homes.
func HomeRoot(stateDir string) string {
	return filepath.Join(stateDir, "home")
}

// HomeDir returns the home directory for a specific user.
func HomeDir(stateDir, username string) string {
	return filepath.Join(HomeRoot(stateDir), username)
}

// CodexDir returns the .codex directory for a user.
func CodexDir(stateDir, username string) string {
	return filepath.Join(HomeDir(stateDir, username), ".codex")
}

// AuthPath returns the auth.json path for a user.
func AuthPath(stateDir, username string) string {
	return filepath.Join(CodexDir(stateDir, username), "auth.json")
}

// EnsureHome creates a user home directory and seeds it from skel templates.
func EnsureHome(stateDir, username, skelDir string, data TemplateData) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("username is required")
	}
	data = normalizeTemplateData(data)
	homeRoot := HomeRoot(stateDir)
	if err := ensureDir(homeRoot, 0o700); err != nil {
		return "", fmt.Errorf("home root %q: %w", homeRoot, err)
	}
	target := HomeDir(stateDir, username)
	exists := false
	if info, err := os.Stat(target); err == nil {
		exists = info.IsDir()
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if !exists {
		if err := os.MkdirAll(target, 0o700); err != nil {
			return "", err
		}
		if err := CopySkel(skelDir, target, data); err != nil {
			return "", err
		}
	}
	if err := ensureCodexConfig(target, data); err != nil {
		return "", err
	}
	if err := ensureDir(filepath.Join(target, ".conan2"), 0o700); err != nil {
		return "", err
	}
	if err := ensureSSHPaths(target); err != nil {
		return "", err
	}
	return target, nil
}

// CopySkel copies a skel directory into a destination, rendering .tmpl files.
func CopySkel(skelDir, destDir string, data TemplateData) error {
	data = normalizeTemplateData(data)
	if strings.TrimSpace(skelDir) == "" {
		return nil
	}
	info, err := os.Stat(skelDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(skelDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == skelDir {
			return nil
		}
		rel, err := filepath.Rel(skelDir, p)
		if err != nil {
			return err
		}
		name := d.Name()
		if name == ".gitkeep" || name == ".keep" || name == "placeholder.txt" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.HasSuffix(target, ".tmpl") {
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rendered, err := renderTemplate(p, raw, data)
			if err != nil {
				return err
			}
			target = strings.TrimSuffix(target, ".tmpl")
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			return os.WriteFile(target, rendered, 0o600)
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			return err
		}
		return dst.Close()
	})
}

func ensureCodexConfig(homeDir string, data TemplateData) error {
	if strings.TrimSpace(homeDir) == "" {
		return errors.New("home directory is required")
	}
	data = normalizeTemplateData(data)
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(codexDir, "config.toml")
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rendered, err := renderTemplate("config.toml", []byte(defaultCodexConfigTemplate), data)
	if err != nil {
		return err
	}
	return os.WriteFile(target, rendered, 0o600)
}

func ensureSSHPaths(homeDir string) error {
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	knownHosts := filepath.Join(sshDir, "known_hosts")
	if _, err := os.Stat(knownHosts); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(knownHosts, []byte(""), 0o644)
}

func normalizeTemplateData(data TemplateData) TemplateData {
	if strings.TrimSpace(data.Codex.Model) == "" {
		data.Codex.Model = "gpt-5.2-codex"
	}
	if strings.TrimSpace(data.Codex.ModelReasoningEffort) == "" {
		data.Codex.ModelReasoningEffort = "medium"
	}
	if strings.TrimSpace(data.Codex.ApprovalPolicy) == "" {
		data.Codex.ApprovalPolicy = "on-request"
	}
	if strings.TrimSpace(data.Codex.SandboxMode) == "" {
		data.Codex.SandboxMode = "danger-full-access"
	}
	return data
}

func renderTemplate(name string, raw []byte, data TemplateData) ([]byte, error) {
	tpl, err := template.New(filepath.Base(name)).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return []byte(buf.String()), nil
}

func ensureDir(path string, mode fs.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is required")
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}
