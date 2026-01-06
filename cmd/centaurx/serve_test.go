package main

import (
	"testing"

	"pkt.systems/centaurx/internal/appconfig"
)

func TestValidateRunnerConfigRejectsHostRepoRoot(t *testing.T) {
	cfg := appconfig.Config{
		Runner: appconfig.RunnerConfig{
			Runtime:      "podman",
			RepoRoot:     "/home/tester/.centaurx/repos",
			HostRepoRoot: "/home/tester/.centaurx/repos",
			Podman: appconfig.PodmanConfig{
				Address: "unix:///run/user/1000/podman/podman.sock",
			},
		},
	}
	if err := validateRunnerConfig(cfg); err == nil {
		t.Fatalf("expected validation error for runner.repo_root pointing at host path")
	}
}
