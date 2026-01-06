package command

import (
	"strings"
)

// Command represents a parsed slash command.
type Command struct {
	Name      string
	Args      []string
	Raw       string
	Remainder string
}

// Parse parses a line and returns a Command if it starts with "/".
func Parse(input string) (Command, bool) {
	trimmed := strings.TrimLeft(input, " \t")
	if !strings.HasPrefix(trimmed, "/") {
		return Command{}, false
	}
	raw := strings.TrimSpace(trimmed[1:])
	if raw == "" {
		return Command{Name: "", Raw: ""}, true
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return Command{Name: "", Raw: raw}, true
	}
	name := strings.ToLower(fields[0])
	args := []string{}
	if len(fields) > 1 {
		args = fields[1:]
	}
	remainder := remainderAfterTokens(raw, 1)
	return Command{
		Name:      name,
		Args:      args,
		Raw:       raw,
		Remainder: remainder,
	}, true
}

func remainderAfterTokens(raw string, count int) string {
	i := 0
	remaining := count
	for remaining > 0 && i < len(raw) {
		for i < len(raw) && isSpace(raw[i]) {
			i++
		}
		for i < len(raw) && !isSpace(raw[i]) {
			i++
		}
		remaining--
	}
	if i >= len(raw) {
		return ""
	}
	return strings.TrimSpace(raw[i:])
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
