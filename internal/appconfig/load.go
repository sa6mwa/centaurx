package appconfig

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Load reads configuration from the provided path. If path is empty, uses DefaultConfigPath.
func Load(path string) (Config, error) {
	if path == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return Config{}, err
		}
		path = defaultPath
	}

	cfg, err := DefaultConfig()
	if err != nil {
		return Config{}, err
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("config_version", cfg.ConfigVersion)
	v.SetDefault("repo_root", cfg.RepoRoot)
	v.SetDefault("state_dir", cfg.StateDir)
	v.SetDefault("models.default", cfg.Models.Default)
	v.SetDefault("models.allowed", cfg.Models.Allowed)
	v.SetDefault("service.buffer_max_lines", cfg.Service.BufferMaxLines)
	v.SetDefault("runner.runtime", cfg.Runner.Runtime)
	v.SetDefault("runner.image", cfg.Runner.Image)
	v.SetDefault("runner.sock_dir", cfg.Runner.SockDir)
	v.SetDefault("runner.repo_root", cfg.Runner.RepoRoot)
	v.SetDefault("runner.host_repo_root", cfg.Runner.HostRepoRoot)
	v.SetDefault("runner.host_state_dir", cfg.Runner.HostStateDir)
	v.SetDefault("runner.socket_path", cfg.Runner.SocketPath)
	v.SetDefault("runner.binary", cfg.Runner.Binary)
	v.SetDefault("runner.args", cfg.Runner.Args)
	v.SetDefault("runner.env", cfg.Runner.Env)
	v.SetDefault("runner.git_ssh_debug", cfg.Runner.GitSSHDebug)
	v.SetDefault("runner.idle_timeout_hours", cfg.Runner.IdleTimeout)
	v.SetDefault("runner.keepalive_interval_seconds", cfg.Runner.KeepaliveIntervalSeconds)
	v.SetDefault("runner.keepalive_misses", cfg.Runner.KeepaliveMisses)
	v.SetDefault("runner.build_timeout_minutes", cfg.Runner.BuildTimeout)
	v.SetDefault("runner.pull_timeout_minutes", cfg.Runner.PullTimeout)
	v.SetDefault("runner.podman.address", cfg.Runner.Podman.Address)
	v.SetDefault("runner.podman.userns_mode", cfg.Runner.Podman.UserNSMode)
	v.SetDefault("runner.containerd.address", cfg.Runner.Containerd.Address)
	v.SetDefault("runner.containerd.namespace", cfg.Runner.Containerd.Namespace)
	v.SetDefault("runner.buildkit.address", cfg.Runner.BuildKit.Address)
	v.SetDefault("http.addr", cfg.HTTP.Addr)
	v.SetDefault("http.session_cookie", cfg.HTTP.SessionCookie)
	v.SetDefault("http.session_ttl_hours", cfg.HTTP.SessionTTLHours)
	v.SetDefault("http.base_url", cfg.HTTP.BaseURL)
	v.SetDefault("http.base_path", cfg.HTTP.BasePath)
	v.SetDefault("http.initial_buffer_lines", cfg.HTTP.InitialBufferLines)
	v.SetDefault("http.ui_max_buffer_lines", cfg.HTTP.UIMaxBufferLines)
	v.SetDefault("ssh.addr", cfg.SSH.Addr)
	v.SetDefault("ssh.host_key_path", cfg.SSH.HostKeyPath)
	v.SetDefault("ssh.key_store_path", cfg.SSH.KeyStorePath)
	v.SetDefault("ssh.key_dir", cfg.SSH.KeyDir)
	v.SetDefault("ssh.agent_dir", cfg.SSH.AgentDir)
	v.SetDefault("auth.user_file", cfg.Auth.UserFile)
	v.SetDefault("auth.seed_users", cfg.Auth.SeedUsers)
	v.SetDefault("logging.disable_audit_trails", cfg.Logging.DisableAuditTrails)

	configLoaded := false
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, err
		}
	} else {
		configLoaded = true
	}

	if configLoaded {
		if !v.IsSet("config_version") {
			return Config{}, fmt.Errorf("config_version is required; expected %d", CurrentConfigVersion)
		}
		if v.GetInt("config_version") != CurrentConfigVersion {
			return Config{}, fmt.Errorf("unsupported config_version %d; expected %d", v.GetInt("config_version"), CurrentConfigVersion)
		}
		if v.IsSet("runner.use_stdin_prompt") {
			return Config{}, fmt.Errorf("runner.use_stdin_prompt is not supported; prompts must use stdin")
		}
		if v.IsSet("http.enable_websockets") {
			return Config{}, fmt.Errorf("http.enable_websockets is no longer supported")
		}
		if !v.IsSet("runner.runtime") {
			return Config{}, fmt.Errorf("runner.runtime is required for config_version %d", CurrentConfigVersion)
		}
		if !v.IsSet("runner.image") {
			return Config{}, fmt.Errorf("runner.image is required for config_version %d", CurrentConfigVersion)
		}
		if !v.IsSet("runner.sock_dir") {
			return Config{}, fmt.Errorf("runner.sock_dir is required for config_version %d", CurrentConfigVersion)
		}
		if !v.IsSet("runner.repo_root") {
			return Config{}, fmt.Errorf("runner.repo_root is required for config_version %d", CurrentConfigVersion)
		}
		switch v.GetString("runner.runtime") {
		case "podman":
			if !v.IsSet("runner.podman.address") {
				return Config{}, fmt.Errorf("runner.podman.address is required for config_version %d", CurrentConfigVersion)
			}
		case "containerd":
			if !v.IsSet("runner.containerd.address") {
				return Config{}, fmt.Errorf("runner.containerd.address is required for config_version %d", CurrentConfigVersion)
			}
			if !v.IsSet("runner.containerd.namespace") {
				return Config{}, fmt.Errorf("runner.containerd.namespace is required for config_version %d", CurrentConfigVersion)
			}
		default:
			return Config{}, fmt.Errorf("unsupported runner.runtime %q", v.GetString("runner.runtime"))
		}
		if !v.IsSet("ssh.key_store_path") {
			return Config{}, fmt.Errorf("ssh.key_store_path is required for config_version %d", CurrentConfigVersion)
		}
		if !v.IsSet("ssh.key_dir") {
			return Config{}, fmt.Errorf("ssh.key_dir is required for config_version %d", CurrentConfigVersion)
		}
		if !v.IsSet("ssh.agent_dir") {
			return Config{}, fmt.Errorf("ssh.agent_dir is required for config_version %d", CurrentConfigVersion)
		}
	}

	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	expandConfigEnv(&cfg)
	if err := validateHTTPConfig(cfg.HTTP); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateHTTPConfig(cfg HTTPConfig) error {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("http.base_url must include scheme and host (e.g. https://example.com)")
		}
	}
	basePath := strings.TrimSpace(cfg.BasePath)
	if basePath != "" {
		if strings.Contains(basePath, "://") {
			return fmt.Errorf("http.base_path must be a path prefix, not a URL")
		}
		if strings.ContainsAny(basePath, "?#") {
			return fmt.Errorf("http.base_path must not include query or fragment")
		}
	}
	return nil
}

func expandConfigEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.RepoRoot = expandEnv(cfg.RepoRoot)
	cfg.StateDir = expandEnv(cfg.StateDir)
	cfg.Runner.SocketPath = expandEnv(cfg.Runner.SocketPath)
	cfg.Runner.SockDir = expandEnv(cfg.Runner.SockDir)
	cfg.Runner.RepoRoot = expandEnv(cfg.Runner.RepoRoot)
	cfg.Runner.HostRepoRoot = expandEnv(cfg.Runner.HostRepoRoot)
	cfg.Runner.HostStateDir = expandEnv(cfg.Runner.HostStateDir)
	cfg.Runner.Binary = expandEnv(cfg.Runner.Binary)
	cfg.Runner.Podman.Address = expandEnv(cfg.Runner.Podman.Address)
	cfg.Runner.Containerd.Address = expandEnv(cfg.Runner.Containerd.Address)
	cfg.Runner.BuildKit.Address = expandEnv(cfg.Runner.BuildKit.Address)
	cfg.SSH.HostKeyPath = expandEnv(cfg.SSH.HostKeyPath)
	cfg.SSH.KeyStorePath = expandEnv(cfg.SSH.KeyStorePath)
	cfg.SSH.KeyDir = expandEnv(cfg.SSH.KeyDir)
	cfg.SSH.AgentDir = expandEnv(cfg.SSH.AgentDir)
	cfg.Auth.UserFile = expandEnv(cfg.Auth.UserFile)
}

func expandEnv(value string) string {
	if value == "" {
		return value
	}
	return os.Expand(value, func(key string) string {
		if key == "" {
			return ""
		}
		if val, ok := lookupEnv(key); ok {
			return val
		}
		return "$" + key
	})
}

func lookupEnv(key string) (string, bool) {
	if val, ok := os.LookupEnv(key); ok {
		return val, true
	}
	switch key {
	case "UID":
		return fmt.Sprintf("%d", os.Getuid()), true
	case "GID":
		return fmt.Sprintf("%d", os.Getgid()), true
	}
	return "", false
}

// WriteDefault writes the default config to the target path.
func WriteDefault(path string, overwrite bool) (string, error) {
	if path == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return "", err
		}
		path = defaultPath
	}

	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config already exists at %s", path)
		}
	}

	cfg, err := DefaultConfig()
	if err != nil {
		return "", err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
