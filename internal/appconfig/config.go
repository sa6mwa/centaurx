package appconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"pkt.systems/centaurx/schema"
)

// Config is the top-level application configuration.
type Config struct {
	ConfigVersion int           `mapstructure:"config_version" yaml:"config_version"`
	RepoRoot      string        `mapstructure:"repo_root" yaml:"repo_root"`
	StateDir      string        `mapstructure:"state_dir" yaml:"state_dir"`
	Models        ModelsConfig  `mapstructure:"models" yaml:"models"`
	Service       ServiceConfig `mapstructure:"service" yaml:"service"`
	Runner        RunnerConfig  `mapstructure:"runner" yaml:"runner"`
	HTTP          HTTPConfig    `mapstructure:"http" yaml:"http"`
	SSH           SSHConfig     `mapstructure:"ssh" yaml:"ssh"`
	Auth          AuthConfig    `mapstructure:"auth" yaml:"auth"`
	Logging       LoggingConfig `mapstructure:"logging" yaml:"logging"`
}

// CurrentConfigVersion marks the supported config version.
const CurrentConfigVersion = 4

// ModelsConfig controls allowed and default LLM models.
type ModelsConfig struct {
	Default string   `mapstructure:"default" yaml:"default"`
	Allowed []string `mapstructure:"allowed" yaml:"allowed"`
}

// ServiceConfig controls core service behavior.
type ServiceConfig struct {
	BufferMaxLines int `mapstructure:"buffer_max_lines" yaml:"buffer_max_lines"`
}

// RunnerConfig configures the runner backend and image settings.
type RunnerConfig struct {
	Runtime                  string            `mapstructure:"runtime" yaml:"runtime"`
	Image                    string            `mapstructure:"image" yaml:"image"`
	SockDir                  string            `mapstructure:"sock_dir" yaml:"sock_dir"`
	RepoRoot                 string            `mapstructure:"repo_root" yaml:"repo_root"`
	HostRepoRoot             string            `mapstructure:"host_repo_root" yaml:"host_repo_root"`
	HostStateDir             string            `mapstructure:"host_state_dir" yaml:"host_state_dir"`
	SocketPath               string            `mapstructure:"socket_path" yaml:"socket_path"`
	Binary                   string            `mapstructure:"binary" yaml:"binary"`
	Args                     []string          `mapstructure:"args" yaml:"args"`
	Env                      map[string]string `mapstructure:"env" yaml:"env"`
	GitSSHDebug              bool              `mapstructure:"git_ssh_debug" yaml:"git_ssh_debug"`
	IdleTimeout              int               `mapstructure:"idle_timeout_hours" yaml:"idle_timeout_hours"`
	KeepaliveIntervalSeconds int               `mapstructure:"keepalive_interval_seconds" yaml:"keepalive_interval_seconds"`
	KeepaliveMisses          int               `mapstructure:"keepalive_misses" yaml:"keepalive_misses"`
	Podman                   PodmanConfig      `mapstructure:"podman" yaml:"podman"`
	Containerd               ContainerdConfig  `mapstructure:"containerd" yaml:"containerd"`
	BuildKit                 BuildKitConfig    `mapstructure:"buildkit" yaml:"buildkit"`
	BuildTimeout             int               `mapstructure:"build_timeout_minutes" yaml:"build_timeout_minutes"`
	PullTimeout              int               `mapstructure:"pull_timeout_minutes" yaml:"pull_timeout_minutes"`
}

// HTTPConfig configures the HTTP server.
type HTTPConfig struct {
	Addr               string `mapstructure:"addr" yaml:"addr"`
	SessionCookie      string `mapstructure:"session_cookie" yaml:"session_cookie"`
	SessionTTLHours    int    `mapstructure:"session_ttl_hours" yaml:"session_ttl_hours"`
	BaseURL            string `mapstructure:"base_url" yaml:"base_url"`
	BasePath           string `mapstructure:"base_path" yaml:"base_path"`
	InitialBufferLines int    `mapstructure:"initial_buffer_lines" yaml:"initial_buffer_lines"`
	UIMaxBufferLines   int    `mapstructure:"ui_max_buffer_lines" yaml:"ui_max_buffer_lines"`
}

// SSHConfig configures the SSH server.
type SSHConfig struct {
	Addr         string `mapstructure:"addr" yaml:"addr"`
	HostKeyPath  string `mapstructure:"host_key_path" yaml:"host_key_path"`
	KeyStorePath string `mapstructure:"key_store_path" yaml:"key_store_path"`
	KeyDir       string `mapstructure:"key_dir" yaml:"key_dir"`
	AgentDir     string `mapstructure:"agent_dir" yaml:"agent_dir"`
}

// AuthConfig configures auth storage and seed users.
type AuthConfig struct {
	UserFile  string     `mapstructure:"user_file" yaml:"user_file"`
	SeedUsers []SeedUser `mapstructure:"seed_users" yaml:"seed_users"`
}

// LoggingConfig controls audit logging behavior.
type LoggingConfig struct {
	DisableAuditTrails bool `mapstructure:"disable_audit_trails" yaml:"disable_audit_trails"`
}

// SeedUser seeds a user record in the auth store.
type SeedUser struct {
	Username     string `mapstructure:"username" yaml:"username"`
	PasswordHash string `mapstructure:"password_hash" yaml:"password_hash"`
	TOTPSecret   string `mapstructure:"totp_secret" yaml:"totp_secret"`
}

// ContainerdConfig configures the containerd runtime endpoint.
type ContainerdConfig struct {
	Address   string `mapstructure:"address" yaml:"address"`
	Namespace string `mapstructure:"namespace" yaml:"namespace"`
}

// PodmanConfig configures the podman runtime endpoint.
type PodmanConfig struct {
	Address    string `mapstructure:"address" yaml:"address"`
	UserNSMode string `mapstructure:"userns_mode" yaml:"userns_mode"`
}

// BuildKitConfig configures the BuildKit endpoint.
type BuildKitConfig struct {
	Address string `mapstructure:"address" yaml:"address"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	uid := os.Getuid()
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run", "user", fmt.Sprintf("%d", uid))
	}
	return Config{
		ConfigVersion: CurrentConfigVersion,
		RepoRoot:      filepath.Join(home, ".centaurx", "repos"),
		StateDir:      filepath.Join(home, ".centaurx", "state"),
		Models: ModelsConfig{
			Default: "gpt-5.2-codex",
			Allowed: []string{"gpt-5.2-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini"},
		},
		Service: ServiceConfig{
			BufferMaxLines: schema.DefaultBufferMaxLines,
		},
		Runner: RunnerConfig{
			Runtime:                  "podman",
			Image:                    "docker.io/pktsystems/centaurxrunner:latest",
			SockDir:                  filepath.Join(home, ".centaurx", "state", "runner"),
			RepoRoot:                 "/repos",
			HostRepoRoot:             "",
			HostStateDir:             "",
			SocketPath:               filepath.Join(home, ".centaurx", "state", "runner.sock"),
			Binary:                   "codex",
			Args:                     []string{},
			Env:                      map[string]string{},
			GitSSHDebug:              false,
			IdleTimeout:              8,
			KeepaliveIntervalSeconds: 10,
			KeepaliveMisses:          3,
			BuildTimeout:             20,
			PullTimeout:              5,
			Podman: PodmanConfig{
				Address:    fmt.Sprintf("unix://%s", filepath.Join(runtimeDir, "podman", "podman.sock")),
				UserNSMode: "keep-id",
			},
			Containerd: ContainerdConfig{
				Address:   fmt.Sprintf("unix://%s", filepath.Join(runtimeDir, "containerd", "containerd.sock")),
				Namespace: "centaurx",
			},
			BuildKit: BuildKitConfig{
				Address: "",
			},
		},
		HTTP: HTTPConfig{
			Addr:               ":27480",
			SessionCookie:      "centaurx_session",
			SessionTTLHours:    720,
			BaseURL:            "",
			BasePath:           "",
			InitialBufferLines: 200,
			UIMaxBufferLines:   2000,
		},
		SSH: SSHConfig{
			Addr:         ":27422",
			HostKeyPath:  filepath.Join(home, ".centaurx", "ssh_host_key"),
			KeyStorePath: filepath.Join(home, ".centaurx", "state", "ssh", "keys.bundle"),
			KeyDir:       filepath.Join(home, ".centaurx", "state", "ssh", "keys"),
			AgentDir:     filepath.Join(home, ".centaurx", "state", "ssh", "agent"),
		},
		Auth: AuthConfig{
			UserFile: filepath.Join(home, ".centaurx", "users.json"),
			SeedUsers: []SeedUser{
				{
					Username:     "admin",
					PasswordHash: "$2a$12$PyjGUD8qnJie1MULQVHJdu9zuS/juh5W5RtDUVHv5HFb.62gNnY/q",
					TOTPSecret:   "JBSWY3DPEHPK3PXP",
				},
			},
		},
		Logging: LoggingConfig{
			DisableAuditTrails: false,
		},
	}, nil
}

// DefaultConfigPath returns the standard config path.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".centaurx", "config.yaml"), nil
}
