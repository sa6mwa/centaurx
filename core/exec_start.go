package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

type gitSummary struct {
	branch      string
	remotes     []string
	statusLines []string
}

func buildExecStartLines(now time.Time, tab *tab, summary gitSummary) []string {
	labelWidth := maxLabelWidth([]string{"Repository", "Branch", "Remote", "Git status", "Model", "Session"})
	repoLabel := ""
	session := ""
	model := schema.ModelID("")
	effort := schema.ModelReasoningEffort("")
	if tab != nil {
		repoLabel = string(tab.Repo.Name)
		session = string(tab.SessionID)
		model = tab.Model
		effort = tab.ModelReasoningEffort
	}
	if strings.TrimSpace(repoLabel) == "" {
		repoLabel = "(unknown)"
	}
	if strings.TrimSpace(session) == "" {
		session = "(new)"
	}

	lines := []string{
		schema.WorkedForMarker + fmt.Sprintf("%s Starting codex exec", now.Format("15:04:05")),
	}
	lines = append(lines, formatLabeledLines("Repository", []string{repoLabel}, labelWidth)...)
	lines = append(lines, formatLabeledLines("Branch", []string{summary.branch}, labelWidth)...)
	lines = append(lines, formatLabeledLines("Remote", summary.remotes, labelWidth)...)
	lines = append(lines, formatLabeledLines("Git status", summary.statusLines, labelWidth)...)
	lines = append(lines, formatLabeledLines("Model", []string{schema.FormatModelWithReasoning(model, effort)}, labelWidth)...)
	lines = append(lines, formatLabeledLines("Session", []string{session}, labelWidth)...)
	return lines
}

func collectGitSummary(ctx context.Context, runner Runner, workingDir, sshAuthSock string) gitSummary {
	summary := gitSummary{
		branch:      "(unknown)",
		remotes:     []string{"(unavailable)"},
		statusLines: []string{"(unavailable)"},
	}
	if runner == nil {
		return summary
	}

	branchLines, err := runCommandLines(ctx, runner, RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     "git rev-parse --abbrev-ref HEAD",
		UseShell:    false,
		SSHAuthSock: sshAuthSock,
	})
	if err == nil && len(branchLines) > 0 {
		branch := strings.TrimSpace(branchLines[0])
		if branch == "" {
			branch = "(unknown)"
		} else if branch == "HEAD" {
			branch = "(detached)"
		}
		summary.branch = branch
	}

	remoteLines, err := runCommandLines(ctx, runner, RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     "git remote -v",
		UseShell:    false,
		SSHAuthSock: sshAuthSock,
	})
	if err == nil {
		parsed := parseGitRemotes(remoteLines)
		if len(parsed) == 0 {
			summary.remotes = []string{"(none)"}
		} else {
			summary.remotes = parsed
		}
	}

	statusLines, err := runCommandLines(ctx, runner, RunCommandRequest{
		WorkingDir:  workingDir,
		Command:     "git status --short",
		UseShell:    false,
		SSHAuthSock: sshAuthSock,
	})
	if err == nil {
		statusLines = trimEmptyLines(statusLines)
		if len(statusLines) == 0 {
			summary.statusLines = []string{"(working tree clean)"}
		} else {
			summary.statusLines = statusLines
		}
	}

	return summary
}

func parseGitRemotes(lines []string) []string {
	type remoteInfo struct {
		fetch string
		push  string
	}
	remotes := make(map[string]*remoteInfo)
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		url := fields[1]
		kind := strings.Trim(fields[2], "()")
		info := remotes[name]
		if info == nil {
			info = &remoteInfo{}
			remotes[name] = info
			order = append(order, name)
		}
		switch kind {
		case "fetch":
			info.fetch = url
		case "push":
			info.push = url
		}
	}
	if len(remotes) == 0 {
		return nil
	}
	name := ""
	if _, ok := remotes["origin"]; ok {
		name = "origin"
	} else {
		name = order[0]
	}
	info := remotes[name]
	if info == nil {
		return nil
	}
	fetch := info.fetch
	push := info.push
	if fetch == "" {
		fetch = push
	}
	if push == "" {
		push = fetch
	}
	if fetch == "" && push == "" {
		return nil
	}
	if fetch == push {
		return []string{fetch}
	}
	lines = []string{}
	if push != "" {
		lines = append(lines, fmt.Sprintf("%s (push)", push))
	}
	if fetch != "" {
		lines = append(lines, fmt.Sprintf("%s (fetch)", fetch))
	}
	return lines
}

func runCommandLines(ctx context.Context, runner Runner, req RunCommandRequest) ([]string, error) {
	log := pslog.Ctx(ctx)
	if runner == nil {
		log.Debug("exec start command skipped", "reason", "runner unavailable")
		return nil, errors.New("runner not available")
	}
	handle, err := runner.RunCommand(ctx, req)
	if err != nil {
		log.Debug("exec start command failed", "command", req.Command, "err", err)
		return nil, err
	}
	defer func() { _ = handle.Close() }()
	stream := handle.Outputs()
	lines := make([]string, 0, 16)
	for {
		output, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Debug("exec start command stream failed", "command", req.Command, "err", err)
			return lines, err
		}
		if output.Text == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(output.Text, " \t"))
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		log.Debug("exec start command wait failed", "command", req.Command, "err", err)
		return lines, err
	}
	if result.ExitCode != 0 {
		log.Debug("exec start command non-zero exit", "command", req.Command, "exit_code", result.ExitCode)
		return lines, fmt.Errorf("command exited with code %d", result.ExitCode)
	}
	return lines, nil
}

func trimEmptyLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func maxLabelWidth(labels []string) int {
	max := 0
	for _, label := range labels {
		if label == "" {
			continue
		}
		width := len(label) + 1
		if width > max {
			max = width
		}
	}
	return max
}

func formatLabeledLines(label string, values []string, labelWidth int) []string {
	if labelWidth <= 0 {
		labelWidth = len(label) + 1
	}
	if len(values) == 0 {
		values = []string{"(unknown)"}
	}
	lines := make([]string, 0, len(values))
	prefix := fmt.Sprintf("%-*s ", labelWidth, label+":")
	indent := strings.Repeat(" ", len(prefix))
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			value = "(unknown)"
		}
		if i == 0 {
			lines = append(lines, prefix+value)
		} else {
			lines = append(lines, indent+value)
		}
	}
	return lines
}
