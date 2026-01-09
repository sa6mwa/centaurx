package bootstrap

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"pkt.systems/centaurx/internal/appconfig"
)

type podmanSpec struct {
	Spec struct {
		Volumes []struct {
			Name     string `yaml:"name"`
			HostPath *struct {
				Path string `yaml:"path"`
				Type string `yaml:"type"`
			} `yaml:"hostPath"`
		} `yaml:"volumes"`
	} `yaml:"spec"`
}

func TestDefaultRepoBundlePodmanPaths(t *testing.T) {
	files, _, err := DefaultRepoBundle()
	if err != nil {
		t.Fatalf("DefaultRepoBundle: %v", err)
	}
	if len(files.PodmanYAML) == 0 {
		t.Fatal("DefaultRepoBundle returned empty podman.yaml")
	}
	paths := readPodmanHostPaths(t, files.PodmanYAML)
	assertHostPath(t, paths, "centaurx-state", defaultHostStateTemplate)
	assertHostPath(t, paths, "centaurx-repos", defaultHostRepoTemplate)
	assertHostPath(t, paths, "centaurx-config", defaultHostConfigTemplate)
	assertHostPath(t, paths, "podman-sock", defaultPodmanSockTemplate)
}

func TestWriteBootstrapPodmanPaths(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	outputDir := t.TempDir()
	paths, err := WriteBootstrap(outputDir, true, "v1.2.3")
	if err != nil {
		t.Fatalf("WriteBootstrap: %v", err)
	}
	data, err := os.ReadFile(paths.Bundle.PodmanPath)
	if err != nil {
		t.Fatalf("Read podman.yaml: %v", err)
	}
	hostPaths := readPodmanHostPaths(t, data)
	assertHostPath(t, hostPaths, "centaurx-state", filepath.Join(outputDir, "state"))
	assertHostPath(t, hostPaths, "centaurx-repos", filepath.Join(outputDir, "repos"))
	assertHostPath(t, hostPaths, "centaurx-config", filepath.Join(outputDir, "config-for-container.yaml"))
	assertHostPath(t, hostPaths, "podman-sock", defaultPodmanSockPath())
}

func TestDefaultFilesWithoutSeedUsers(t *testing.T) {
	files, _, err := DefaultFiles()
	if err != nil {
		t.Fatalf("DefaultFiles: %v", err)
	}
	cfg := readConfig(t, files.ConfigYAML)
	if len(cfg.Auth.SeedUsers) != 0 {
		t.Fatalf("expected no seed users, got %d", len(cfg.Auth.SeedUsers))
	}
}

func TestDefaultFilesWithSeedUsers(t *testing.T) {
	files, _, err := DefaultFilesWithOptions(Options{SeedUsers: true})
	if err != nil {
		t.Fatalf("DefaultFilesWithOptions: %v", err)
	}
	cfg := readConfig(t, files.ConfigYAML)
	if len(cfg.Auth.SeedUsers) != 1 {
		t.Fatalf("expected 1 seed user, got %d", len(cfg.Auth.SeedUsers))
	}
	if cfg.Auth.SeedUsers[0].Username != "admin" {
		t.Fatalf("expected seed user admin, got %q", cfg.Auth.SeedUsers[0].Username)
	}
}

func TestBootstrapConfigParity(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	hostCfg, err := DefaultHostConfig()
	if err != nil {
		t.Fatalf("DefaultHostConfig: %v", err)
	}
	files, _, err := DefaultFiles()
	if err != nil {
		t.Fatalf("DefaultFiles: %v", err)
	}
	containerCfg := readConfig(t, files.ConfigYAML)

	stripBootstrapPaths(&hostCfg)
	stripBootstrapPaths(&containerCfg)
	if !reflect.DeepEqual(hostCfg, containerCfg) {
		t.Fatalf("host/container configs differ after path normalization")
	}
}

func readPodmanHostPaths(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var spec podmanSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal podman.yaml: %v", err)
	}
	paths := make(map[string]string)
	for _, volume := range spec.Spec.Volumes {
		if volume.HostPath == nil {
			continue
		}
		paths[volume.Name] = volume.HostPath.Path
	}
	return paths
}

func readConfig(t *testing.T, data []byte) appconfig.Config {
	t.Helper()
	var cfg appconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return cfg
}

func assertHostPath(t *testing.T, paths map[string]string, name, expected string) {
	t.Helper()
	path, ok := paths[name]
	if !ok {
		t.Fatalf("missing host path for %s", name)
	}
	if path == "" {
		t.Fatalf("empty host path for %s", name)
	}
	if expected != "" && path != expected {
		t.Fatalf("unexpected host path for %s: %q", name, path)
	}
}

func stripBootstrapPaths(cfg *appconfig.Config) {
	cfg.RepoRoot = ""
	cfg.StateDir = ""
	cfg.Runner.SockDir = ""
	cfg.Runner.RepoRoot = ""
	cfg.Runner.SocketPath = ""
	cfg.Runner.HostRepoRoot = ""
	cfg.Runner.HostStateDir = ""
	cfg.Runner.Podman.Address = ""
	cfg.SSH.HostKeyPath = ""
	cfg.SSH.KeyStorePath = ""
	cfg.SSH.KeyDir = ""
	cfg.SSH.AgentDir = ""
	cfg.Auth.UserFile = ""
	cfg.HTTP.SessionStorePath = ""
}
