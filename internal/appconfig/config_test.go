package appconfig

import "testing"

func TestDefaultConfigGitSSHDebug(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	if cfg.Runner.GitSSHDebug {
		t.Fatalf("expected git ssh debug to default false")
	}
}
