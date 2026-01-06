package sshserver

import (
	"strings"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestRenderTabBarFullWidth(t *testing.T) {
	tabs := []schema.TabSnapshot{
		{ID: "tab1", Name: "alpha"},
		{ID: "tab2", Name: "beta"},
	}
	theme := themeForName("outrun")
	line, _ := renderTabBar(tabs, "tab2", 40, theme, 0)
	if got := visibleWidth(line); got != 40 {
		t.Fatalf("expected tab bar width 40, got %d", got)
	}
	if !strings.Contains(line, ansiBgRGB(theme.TabActiveBG)) {
		t.Fatalf("expected active tab background color sequence")
	}
	if !strings.HasSuffix(line, ansiReset) {
		t.Fatalf("expected tab bar to reset styles")
	}
}

func TestRenderTabBarIndicators(t *testing.T) {
	theme := themeForName("outrun")
	tabs := []schema.TabSnapshot{
		{ID: "tab1", Name: "alpha"},
		{ID: "tab2", Name: "beta"},
		{ID: "tab3", Name: "gamma"},
		{ID: "tab4", Name: "delta"},
		{ID: "tab5", Name: "epsilon"},
	}
	line, _ := renderTabBar(tabs, "tab3", 20, theme, 0)
	if !strings.Contains(line, "<") {
		t.Fatalf("expected left indicator for hidden tabs")
	}
	if !strings.Contains(line, ">") {
		t.Fatalf("expected right indicator for hidden tabs")
	}

	line, _ = renderTabBar(tabs, "tab1", 20, theme, 0)
	if strings.Contains(line, "<") {
		t.Fatalf("did not expect left indicator when at first tab")
	}
	if !strings.Contains(line, ">") {
		t.Fatalf("expected right indicator when more tabs exist")
	}

	line, _ = renderTabBar(tabs, "tab5", 20, theme, 0)
	if !strings.Contains(line, "<") {
		t.Fatalf("expected left indicator when more tabs exist")
	}
	if strings.Contains(line, ">") {
		t.Fatalf("did not expect right indicator when at last tab")
	}
}

func TestRenderTabBarWindowShift(t *testing.T) {
	theme := themeForName("outrun")
	tabs := []schema.TabSnapshot{
		{ID: "tab1", Name: "one"},
		{ID: "tab2", Name: "two"},
		{ID: "tab3", Name: "three"},
		{ID: "tab4", Name: "four"},
		{ID: "tab5", Name: "five"},
	}
	start := 0
	_, start = renderTabBar(tabs, "tab1", 20, theme, start)
	if start != 0 {
		t.Fatalf("expected window start 0, got %d", start)
	}
	_, start = renderTabBar(tabs, "tab2", 20, theme, start)
	if start != 0 {
		t.Fatalf("expected window start to stay 0, got %d", start)
	}
	_, start = renderTabBar(tabs, "tab3", 20, theme, start)
	if start != 0 {
		t.Fatalf("expected window start to stay 0, got %d", start)
	}
	_, start = renderTabBar(tabs, "tab4", 20, theme, start)
	if start != 1 {
		t.Fatalf("expected window to shift right to 1, got %d", start)
	}
	_, start = renderTabBar(tabs, "tab5", 20, theme, start)
	if start != 2 {
		t.Fatalf("expected window to shift right to 2, got %d", start)
	}
	_, start = renderTabBar(tabs, "tab2", 20, theme, start)
	if start != 1 {
		t.Fatalf("expected window to shift left to 1, got %d", start)
	}
}

func TestRenderLineStripsStderrMarker(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.StderrMarker+"stderr output", 80, theme)
	if strings.Contains(line, schema.StderrMarker) {
		t.Fatalf("stderr marker should be stripped from rendered line")
	}
	if !strings.Contains(line, ansiFgRGB(theme.StderrFG)) {
		t.Fatalf("stderr line should include stderr color")
	}
	if !strings.Contains(line, "stderr output") {
		t.Fatalf("stderr text missing from rendered line: %q", line)
	}
}

func TestSanitizeOutputLineStripsAnsiAndControl(t *testing.T) {
	input := "\x1b[2Jhello\rworld\x1b[0m"
	got := sanitizeOutputLine(input)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("expected ANSI escapes removed, got %q", got)
	}
	if strings.Contains(got, "\r") {
		t.Fatalf("expected carriage returns removed, got %q", got)
	}
	if got != "helloworld" {
		t.Fatalf("unexpected sanitize result: %q", got)
	}
}

func TestRenderWorkedLineFullWidth(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.WorkedForMarker+"Worked for 19s", 50, theme)
	if got := visibleWidth(line); got != 50 {
		t.Fatalf("expected worked line width 50, got %d", got)
	}
	if !strings.Contains(line, "Worked for 19s") {
		t.Fatalf("expected worked label in line: %q", line)
	}
}

func TestRenderReasoningMarkdown(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.ReasoningMarker+"**bold** note", 80, theme)
	if !strings.Contains(line, ansiItalic) {
		t.Fatalf("expected reasoning to be italic")
	}
	if !strings.Contains(line, ansiBold) {
		t.Fatalf("expected bold span")
	}
	if !strings.Contains(line, ansiFgRGB(theme.ReasoningFG)) {
		t.Fatalf("expected reasoning color")
	}
	if !strings.Contains(line, ansiFgRGB(theme.ReasoningBold)) {
		t.Fatalf("expected reasoning bold color")
	}
}

func TestRenderAgentMarkdownCode(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.AgentMarker+"`code`", 80, theme)
	if !strings.Contains(line, ansiFgRGB(theme.CodeFG)) {
		t.Fatalf("expected code color for agent markdown")
	}
}

func TestRenderAboutVersionStyles(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.AboutVersionMarker+"pkt.systems/centaurx v0.0.0", 80, theme)
	if !strings.Contains(line, ansiBold) || !strings.Contains(line, ansiItalic) {
		t.Fatalf("expected about version to be bold+italic")
	}
	if strings.Contains(line, schema.AboutVersionMarker) {
		t.Fatalf("about version marker should be stripped from rendered line")
	}
}

func TestRenderAboutLinkStyles(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.AboutLinkMarker+"https://example.com", 80, theme)
	if !strings.Contains(line, ansiItalic) {
		t.Fatalf("expected about link to be italic")
	}
	if !strings.Contains(line, ansiFgRGB(theme.AboutLinkFG)) {
		t.Fatalf("expected about link color")
	}
}

func TestRenderAboutCopyrightStyles(t *testing.T) {
	theme := themeForName("outrun")
	line := renderLine(schema.AboutCopyrightMarker+"Copyright", 80, theme)
	if !strings.Contains(line, ansiFgRGB(theme.AboutCopyrightFG)) {
		t.Fatalf("expected about copyright color")
	}
}

func TestRenderLinesWrapsReasoning(t *testing.T) {
	theme := themeForName("outrun")
	text := strings.Repeat("a", 25)
	lines := renderLines(schema.ReasoningMarker+text, 10, theme)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d", len(lines))
	}
	for i, line := range lines {
		if got := visibleWidth(line); got > 10 {
			t.Fatalf("line %d width %d exceeds limit", i, got)
		}
	}
}

func TestRenderLinesWordWrapsMarkdown(t *testing.T) {
	theme := themeForName("outrun")
	text := schema.AgentMarker + "contact, like walls"
	lines := renderLines(text, 10, theme)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d", len(lines))
	}
	joined := strings.Join(sanitizeLines(lines), "\n")
	if strings.Contains(joined, "lik\ne") {
		t.Fatalf("expected word wrap on spaces, got split: %q", joined)
	}
}

func TestRenderLinesWordWrapsPlain(t *testing.T) {
	theme := themeForName("outrun")
	text := "login pubkeys: 1) ssh-rsa AAAAB3"
	lines := renderLines(text, 10, theme)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d", len(lines))
	}
	joined := strings.Join(sanitizeLines(lines), "\n")
	if strings.Contains(joined, "ssh\n-rsa") {
		t.Fatalf("expected word wrap on spaces, got split: %q", joined)
	}
}

func sanitizeLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, sanitizeOutputLine(line))
	}
	return out
}
