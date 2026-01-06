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
		Prompt:               "hello",
		JSON:                 true,
		Model:                schema.ModelID("gpt-5.2-codex"),
		ModelReasoningEffort: schema.ModelReasoningEffort("medium"),
		ResumeSessionID:      "session-1",
	}
	args := buildExecArgs(cfg, req)
	want := []string{
		"exec",
		"--json",
		"--model",
		"gpt-5.2-codex",
		"-c",
		"model_reasoning_effort=medium",
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
		Prompt:               "hello",
		JSON:                 true,
		ModelReasoningEffort: schema.ModelReasoningEffort("medium"),
	}
	args := buildExecArgs(cfg, req)
	want := []string{"exec", "--json", "-c", "model_reasoning_effort=medium", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\nwant: %#v\ngot:  %#v", want, args)
	}
}
