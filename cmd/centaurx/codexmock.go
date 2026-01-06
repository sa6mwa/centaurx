package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newCodexMockCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "codex-mock exec [--json] [--model <id>] [--seed <n>] [--scenario <name>] [--delay-ms <n>] [--linger-ms <n>] [resume <id>] [prompt|-]",
		Short:         "Mock codex exec --json streams for testing",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexMock(args, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func runCodexMock(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseMockArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return err
	}

	prompt, err := resolveMockPrompt(cfg.prompt, stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return err
	}
	cfg.prompt = prompt

	if !cfg.seedSet {
		cfg.seed = hashSeed(cfg.prompt, cfg.resumeID, cfg.model, cfg.scenario)
	}

	threadID := cfg.resumeID
	if threadID == "" {
		threadID = mockThreadID(cfg.seed)
	}

	writer := bufio.NewWriter(stdout)
	defer func() { _ = writer.Flush() }()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM)
	signalSeen := make(chan os.Signal, 1)
	go func() {
		sig := <-sigCh
		signalSeen <- sig
	}()

	if !cfg.jsonOutput {
		_, _ = fmt.Fprintln(writer, mockAgentMessage(cfg.seed, cfg.prompt))
		return nil
	}

	if err := writeEvent(writer, map[string]any{
		"type":      "thread.started",
		"thread_id": threadID,
	}); err != nil {
		return err
	}

	if err := writeEvent(writer, map[string]any{"type": "turn.started"}); err != nil {
		return err
	}

	scenarios := buildScenarios()
	activeScenario, err := pickScenario(cfg, scenarios)
	if err != nil {
		return err
	}

	if err := activeScenario.run(cfg, writer); err != nil {
		return err
	}

	select {
	case sig := <-signalSeen:
		if err := emitSignalError(writer, sig); err != nil {
			return err
		}
		return nil
	default:
	}

	if err := writeEvent(writer, map[string]any{
		"type": "turn.completed",
		"usage": map[string]any{
			"input_tokens":        int(len(cfg.prompt)) + 12,
			"cached_input_tokens": int(len(cfg.prompt)) / 3,
			"output_tokens":       int(20 + cfg.seed%50),
		},
	}); err != nil {
		return err
	}

	if cfg.linger > 0 {
		timer := time.NewTimer(cfg.linger)
		select {
		case sig := <-signalSeen:
			timer.Stop()
			if err := emitSignalError(writer, sig); err != nil {
				return err
			}
			return nil
		case <-timer.C:
		}
	}
	return nil
}

type mockConfig struct {
	jsonOutput bool
	model      string
	resumeID   string
	prompt     string
	seed       uint64
	seedSet    bool
	scenario   string
	delay      time.Duration
	linger     time.Duration
}

type mockScenario struct {
	name string
	run  func(cfg mockConfig, w *bufio.Writer) error
}

func parseMockArgs(args []string) (mockConfig, error) {
	if len(args) == 0 {
		return mockConfig{}, errors.New("usage: codex-mock exec [--json] [--model <id>] [--seed <n>] [--scenario <name>] [--delay-ms <n>] [--linger-ms <n>] [resume <id>] [prompt|-]")
	}
	if args[0] != "exec" {
		return mockConfig{}, fmt.Errorf("unsupported command: %s", args[0])
	}
	cfg := mockConfig{
		delay:  30 * time.Millisecond,
		linger: 0,
	}
	args = args[1:]
	for len(args) > 0 {
		if args[0] == "resume" {
			args = args[1:]
			if len(args) == 0 {
				return mockConfig{}, errors.New("resume requires a session id")
			}
			cfg.resumeID = args[0]
			args = args[1:]
			if len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
				return mockConfig{}, fmt.Errorf("unexpected argument %q found", args[0])
			}
			cfg.prompt = strings.Join(args, " ")
			return cfg, nil
		}
		if args[0] == "-" {
			cfg.prompt = "-"
			return cfg, nil
		}
		if !strings.HasPrefix(args[0], "-") {
			cfg.prompt = strings.Join(args, " ")
			return cfg, nil
		}
		switch args[0] {
		case "--json":
			cfg.jsonOutput = true
			args = args[1:]
		case "--model":
			if len(args) < 2 {
				return mockConfig{}, errors.New("--model requires a value")
			}
			cfg.model = args[1]
			args = args[2:]
		case "--seed":
			if len(args) < 2 {
				return mockConfig{}, errors.New("--seed requires a value")
			}
			val, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return mockConfig{}, fmt.Errorf("invalid --seed: %w", err)
			}
			cfg.seed = val
			cfg.seedSet = true
			args = args[2:]
		case "--scenario":
			if len(args) < 2 {
				return mockConfig{}, errors.New("--scenario requires a value")
			}
			cfg.scenario = args[1]
			args = args[2:]
		case "--delay-ms":
			if len(args) < 2 {
				return mockConfig{}, errors.New("--delay-ms requires a value")
			}
			val, err := strconv.Atoi(args[1])
			if err != nil || val < 0 {
				return mockConfig{}, errors.New("invalid --delay-ms")
			}
			cfg.delay = time.Duration(val) * time.Millisecond
			args = args[2:]
		case "--linger-ms":
			if len(args) < 2 {
				return mockConfig{}, errors.New("--linger-ms requires a value")
			}
			val, err := strconv.Atoi(args[1])
			if err != nil || val < 0 {
				return mockConfig{}, errors.New("invalid --linger-ms")
			}
			cfg.linger = time.Duration(val) * time.Millisecond
			args = args[2:]
		default:
			return mockConfig{}, fmt.Errorf("unsupported flag: %s", args[0])
		}
	}
	return cfg, nil
}

func resolveMockPrompt(arg string, stdin io.Reader) (string, error) {
	if arg == "-" {
		return readStdinPrompt(stdin)
	}
	if strings.TrimSpace(arg) != "" {
		return arg, nil
	}
	if isTerminalReader(stdin) {
		return "", errors.New("no prompt provided")
	}
	return readStdinPrompt(stdin)
}

func readStdinPrompt(stdin io.Reader) (string, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read prompt from stdin: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", errors.New("no prompt provided via stdin")
	}
	return prompt, nil
}

func isTerminalReader(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func hashSeed(prompt, resumeID, model, scenario string) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(prompt))
	_, _ = hasher.Write([]byte(resumeID))
	_, _ = hasher.Write([]byte(model))
	_, _ = hasher.Write([]byte(scenario))
	return hasher.Sum64()
}

func mockThreadID(seed uint64) string {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], seed)
	binary.LittleEndian.PutUint64(buf[8:16], seed^0x9e3779b97f4a7c15)
	return "mock-" + hex.EncodeToString(buf[:])
}

func buildScenarios() []mockScenario {
	return []mockScenario{
		{name: "summary", run: scenarioSummary},
		{name: "command", run: scenarioCommand},
		{name: "filechange", run: scenarioFileChange},
		{name: "websearch", run: scenarioWebSearch},
		{name: "todo", run: scenarioTodo},
		{name: "failure", run: scenarioFailure},
	}
}

func pickScenario(cfg mockConfig, scenarios []mockScenario) (mockScenario, error) {
	if cfg.scenario != "" {
		for _, s := range scenarios {
			if s.name == cfg.scenario {
				return s, nil
			}
		}
		return mockScenario{}, fmt.Errorf("unknown scenario: %s", cfg.scenario)
	}
	idx := int(cfg.seed % uint64(len(scenarios)))
	return scenarios[idx], nil
}

func scenarioSummary(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type": "reasoning",
		"text": "Summarizing repo state before answering.",
	}, cfg.delay); err != nil {
		return err
	}
	return writeItem(w, "item.completed", "item_1", map[string]any{
		"type": "agent_message",
		"text": mockAgentMessage(cfg.seed, cfg.prompt),
	}, cfg.delay)
}

func scenarioCommand(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.started", "item_0", map[string]any{
		"type":              "command_execution",
		"command":           "bash -lc ls",
		"aggregated_output": "",
		"exit_code":         nil,
		"status":            "in_progress",
	}, cfg.delay); err != nil {
		return err
	}
	if err := writeItem(w, "item.updated", "item_0", map[string]any{
		"type":              "command_execution",
		"command":           "bash -lc ls",
		"aggregated_output": "README.md\nmain.go\n",
		"exit_code":         nil,
		"status":            "in_progress",
	}, cfg.delay); err != nil {
		return err
	}
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type":              "command_execution",
		"command":           "bash -lc ls",
		"aggregated_output": "README.md\nmain.go\n",
		"exit_code":         0,
		"status":            "completed",
	}, cfg.delay); err != nil {
		return err
	}
	return writeItem(w, "item.completed", "item_1", map[string]any{
		"type": "agent_message",
		"text": "Command finished. Here is a brief summary of the output.",
	}, cfg.delay)
}

func scenarioFileChange(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type": "file_change",
		"changes": []map[string]any{
			{"path": "README.md", "kind": "update"},
			{"path": "main.go", "kind": "add"},
		},
		"status": "completed",
	}, cfg.delay); err != nil {
		return err
	}
	return writeItem(w, "item.completed", "item_1", map[string]any{
		"type": "agent_message",
		"text": "Updated documentation and added a stub main file.",
	}, cfg.delay)
}

func scenarioWebSearch(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.started", "item_0", map[string]any{
		"type":  "web_search",
		"query": "golang jsonl streaming parser",
	}, cfg.delay); err != nil {
		return err
	}
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type":  "web_search",
		"query": "golang jsonl streaming parser",
	}, cfg.delay); err != nil {
		return err
	}
	return writeItem(w, "item.completed", "item_1", map[string]any{
		"type": "agent_message",
		"text": "Found a few approaches; recommending bufio.Reader with ReadBytes.",
	}, cfg.delay)
}

func scenarioTodo(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.started", "item_0", map[string]any{
		"type": "todo_list",
		"items": []map[string]any{
			{"text": "Inspect repo layout", "completed": false},
			{"text": "Run tests", "completed": false},
			{"text": "Summarize findings", "completed": false},
		},
	}, cfg.delay); err != nil {
		return err
	}
	if err := writeItem(w, "item.updated", "item_0", map[string]any{
		"type": "todo_list",
		"items": []map[string]any{
			{"text": "Inspect repo layout", "completed": true},
			{"text": "Run tests", "completed": true},
			{"text": "Summarize findings", "completed": false},
		},
	}, cfg.delay); err != nil {
		return err
	}
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type": "todo_list",
		"items": []map[string]any{
			{"text": "Inspect repo layout", "completed": true},
			{"text": "Run tests", "completed": true},
			{"text": "Summarize findings", "completed": true},
		},
	}, cfg.delay); err != nil {
		return err
	}
	return writeItem(w, "item.completed", "item_1", map[string]any{
		"type": "agent_message",
		"text": "All checklist items complete. Summary follows.",
	}, cfg.delay)
}

func scenarioFailure(cfg mockConfig, w *bufio.Writer) error {
	if err := writeItem(w, "item.completed", "item_0", map[string]any{
		"type": "reasoning",
		"text": "Attempting operation that will fail.",
	}, cfg.delay); err != nil {
		return err
	}
	return writeEvent(w, map[string]any{
		"type": "turn.failed",
		"error": map[string]any{
			"message": "mock failure: simulated turn error",
		},
	})
}

func writeItem(w *bufio.Writer, eventType, id string, item map[string]any, delay time.Duration) error {
	item["id"] = id
	if err := writeEvent(w, map[string]any{
		"type": eventType,
		"item": item,
	}); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil
}

func writeEvent(w *bufio.Writer, event map[string]any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.WriteString("\n"); err != nil {
		return err
	}
	return w.Flush()
}

func emitSignalError(w *bufio.Writer, sig os.Signal) error {
	msg := fmt.Sprintf("mock received %s", sig)
	return writeEvent(w, map[string]any{
		"type":    "error",
		"message": msg,
	})
}

func mockAgentMessage(seed uint64, prompt string) string {
	templates := []string{
		"Mock response: handled request \"%s\".",
		"Mock response: completed task for \"%s\".",
		"Mock response: produced summary for \"%s\".",
		"Mock response: generated output for \"%s\".",
	}
	idx := int(seed % uint64(len(templates)))
	return fmt.Sprintf(templates[idx], prompt)
}
