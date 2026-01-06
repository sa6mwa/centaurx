package codex

import (
	"reflect"
	"testing"

	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
)

func TestBuildExecArgsResumeOrdersFlagsBeforeResume(t *testing.T) {
	cfg := Config{ExtraArgs: []string{"--verbose"}}
	req := core.RunRequest{
		Prompt:          "hello",
		JSON:            true,
		Model:           schema.ModelID("gpt-5.2-codex"),
		ResumeSessionID: "session-1",
	}
	args := buildExecArgs(cfg, req)
	want := []string{
		"exec",
		"--json",
		"--model",
		"gpt-5.2-codex",
		"--verbose",
		"resume",
		"session-1",
		"-",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\nwant: %#v\ngot:  %#v", want, args)
	}
}

func TestBuildExecArgsNewSession(t *testing.T) {
	cfg := Config{}
	req := core.RunRequest{
		Prompt: "hello",
		JSON:   true,
	}
	args := buildExecArgs(cfg, req)
	want := []string{"exec", "--json", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\nwant: %#v\ngot:  %#v", want, args)
	}
}
