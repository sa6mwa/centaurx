package main

import (
	"testing"
)

func TestArgv0Alias(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "cxrunner", base: "cxrunner", want: "runner"},
		{name: "codex-mock", base: "codex-mock", want: "codex-mock"},
		{name: "centaurx-codex-mock", base: "centaurx-codex-mock", want: "codex-mock"},
		{name: "centaurx", base: "centaurx", want: ""},
	}
	for _, tc := range tests {
		if got := argv0Alias(tc.base); got != tc.want {
			t.Fatalf("%s: argv0Alias(%q) = %q, want %q", tc.name, tc.base, got, tc.want)
		}
	}
}

func TestApplyArgv0Alias(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "empty", args: nil, want: nil},
		{name: "no-alias", args: []string{"centaurx", "serve"}, want: []string{"centaurx", "serve"}},
		{name: "cxrunner", args: []string{"cxrunner", "-c", "cfg.yaml"}, want: []string{"cxrunner", "runner", "-c", "cfg.yaml"}},
		{name: "codex-mock", args: []string{"codex-mock", "exec", "-"}, want: []string{"codex-mock", "codex-mock", "exec", "-"}},
	}
	for _, tc := range tests {
		got := applyArgv0Alias(tc.args)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: applyArgv0Alias length = %d, want %d", tc.name, len(got), len(tc.want))
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: applyArgv0Alias[%d] = %q, want %q", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}

func TestIsCodexMockInvocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "codex-mock", args: []string{"centaurx", "codex-mock"}, want: true},
		{name: "runner", args: []string{"centaurx", "runner"}, want: false},
		{name: "empty", args: nil, want: false},
	}
	for _, tc := range tests {
		if got := isCodexMockInvocation(tc.args); got != tc.want {
			t.Fatalf("%s: isCodexMockInvocation(%v) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestRootHasCodexMock(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "codex-mock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include codex-mock")
	}
}

func TestRootHasBuild(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "build" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include build")
	}
}

func TestRootHasVersion(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include version")
	}
}

func TestRootHasDoctor(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "doctor" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include doctor")
	}
}

func TestRootHasDebug(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "debug" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include debug")
	}
}
