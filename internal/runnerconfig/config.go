package runnerconfig

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config defines the runner-only configuration file.
type Config struct {
	SocketPath               string            `yaml:"socket_path"`
	Binary                   string            `yaml:"binary"`
	Args                     []string          `yaml:"args"`
	Env                      map[string]string `yaml:"env"`
	KeepaliveIntervalSeconds int               `yaml:"keepalive_interval_seconds"`
	KeepaliveMisses          int               `yaml:"keepalive_misses"`
}

// Load parses a runner-only config file.
func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.SocketPath = expandEnv(cfg.SocketPath)
	cfg.Binary = expandEnv(cfg.Binary)
	if strings.TrimSpace(cfg.SocketPath) == "" {
		return Config{}, fmt.Errorf("socket_path is required")
	}
	if strings.TrimSpace(cfg.Binary) == "" {
		cfg.Binary = "codex"
	}
	if cfg.KeepaliveIntervalSeconds == 0 {
		cfg.KeepaliveIntervalSeconds = 10
	}
	if cfg.KeepaliveMisses == 0 {
		cfg.KeepaliveMisses = 3
	}
	return cfg, nil
}

func expandEnv(value string) string {
	if value == "" {
		return value
	}
	return os.Expand(value, func(key string) string {
		if key == "" {
			return ""
		}
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		switch key {
		case "UID":
			return fmt.Sprintf("%d", os.Getuid())
		case "GID":
			return fmt.Sprintf("%d", os.Getgid())
		}
		return "$" + key
	})
}
