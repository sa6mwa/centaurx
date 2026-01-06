package format

import (
	"fmt"
	"strings"

	"pkt.systems/centaurx/schema"
)

// PlainRenderer formats events as plain text lines.
type PlainRenderer struct{}

// NewPlainRenderer returns a default plain-text renderer.
func NewPlainRenderer() *PlainRenderer {
	return &PlainRenderer{}
}

// FormatEvent converts an ExecEvent into user-facing lines.
func (p *PlainRenderer) FormatEvent(event schema.ExecEvent) ([]string, error) {
	switch event.Type {
	case schema.EventThreadStarted:
		return nil, nil
	case schema.EventTurnFailed:
		if event.Error != nil && event.Error.Message != "" {
			return []string{fmt.Sprintf("turn failed: %s", event.Error.Message)}, nil
		}
		return []string{"turn failed"}, nil
	case schema.EventError:
		if event.Message != "" {
			return []string{fmt.Sprintf("error: %s", event.Message)}, nil
		}
		return []string{"error: unknown"}, nil
	case schema.EventItemStarted, schema.EventItemUpdated, schema.EventItemCompleted:
		return p.formatItem(event.Type, event.Item), nil
	case schema.EventTurnCompleted, schema.EventTurnStarted:
		return nil, nil
	default:
		return nil, nil
	}
}

func (p *PlainRenderer) formatItem(eventType schema.EventType, item *schema.ItemEvent) []string {
	if item == nil {
		return nil
	}
	switch item.Type {
	case schema.ItemAgentMessage:
		return markLines(schema.AgentMarker, splitLines(item.Text))
	case schema.ItemReasoning:
		if item.Text == "" {
			return nil
		}
		return markLines(schema.ReasoningMarker, splitLines(item.Text))
	case schema.ItemCommandExecution:
		return markLines(schema.CommandMarker, formatCommand(item, eventType))
	case schema.ItemFileChange:
		return formatFileChange(item)
	case schema.ItemWebSearch:
		if item.Query == "" {
			return []string{"web search executed"}
		}
		return []string{fmt.Sprintf("web search: %s", item.Query)}
	case schema.ItemTodoList:
		return formatTodo(item)
	default:
		label := string(item.Type)
		if label == "" {
			label = "item"
		}
		return []string{fmt.Sprintf("%s event", label)}
	}
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func markLines(marker string, lines []string) []string {
	if marker == "" || len(lines) == 0 {
		return lines
	}
	marked := make([]string, 0, len(lines))
	for _, line := range lines {
		marked = append(marked, marker+line)
	}
	return marked
}

func formatCommand(item *schema.ItemEvent, eventType schema.EventType) []string {
	lines := []string{}
	commandLine := ""
	if item.Command != "" {
		commandLine = fmt.Sprintf("$ %s", item.Command)
		lines = append(lines, commandLine)
	}
	if item.AggregatedOutput != "" {
		outputLines := strings.Split(strings.TrimRight(item.AggregatedOutput, "\n"), "\n")
		if commandLine != "" && len(outputLines) > 0 && outputLines[0] == commandLine {
			outputLines = outputLines[1:]
		}
		lines = append(lines, outputLines...)
	}
	if eventType == schema.EventItemCompleted && item.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("exit code: %d", *item.ExitCode))
	}
	return lines
}

func formatFileChange(item *schema.ItemEvent) []string {
	if len(item.Changes) == 0 {
		return []string{"file change"}
	}
	lines := []string{"file changes:"}
	for _, change := range item.Changes {
		label := strings.TrimSpace(change.Kind)
		if label == "" {
			label = "update"
		}
		lines = append(lines, fmt.Sprintf("- %s %s", label, change.Path))
	}
	return lines
}

func formatTodo(item *schema.ItemEvent) []string {
	if len(item.Items) == 0 {
		return []string{"todo list updated"}
	}
	lines := []string{"todo list:"}
	for _, entry := range item.Items {
		prefix := "[ ]"
		if entry.Completed {
			prefix = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", prefix, entry.Text))
	}
	return lines
}
