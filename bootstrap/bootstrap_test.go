package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
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
