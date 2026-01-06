package sshserver

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"pkt.systems/centaurx/internal/markdown"
	"pkt.systems/centaurx/schema"
)

type lineKind int

const (
	lineNormal lineKind = iota
	lineError
	lineStderr
	lineMeta
	lineWorked
	lineAgent
	lineReasoning
	lineCommand
	lineHelp
	lineAboutVersion
	lineAboutCopyright
	lineAboutLink
)

func renderTabBar(tabs []schema.TabSnapshot, active schema.TabID, width int, theme tuiTheme, windowStart int) (string, int) {
	if width <= 0 {
		width = 80
	}
	barStyle := ansiBgRGB(theme.TabBarBG) + ansiFgRGB(theme.TabInactiveFG)
	activeStyle := ansiBgRGB(theme.TabActiveBG) + ansiFgRGB(theme.TabActiveFG) + ansiBold
	inactiveStyle := ansiBgRGB(theme.TabInactiveBG) + ansiFgRGB(theme.TabInactiveFG)
	indicatorStyle := ansiBgRGB(theme.TabBarBG) + ansiFgRGB(theme.TabInactiveFG) + ansiBold

	var b strings.Builder
	b.WriteString(barStyle)
	if len(tabs) == 0 {
		b.WriteString(inactiveStyle)
		b.WriteString(" no tabs ")
		b.WriteString(barStyle)
	} else {
		labels := make([]string, 0, len(tabs))
		widths := make([]int, 0, len(tabs))
		activeIndex := 0
		foundActive := false
		totalWidth := 0
		for i, tab := range tabs {
			name := string(tab.Name)
			if name == "" {
				name = string(tab.Repo.Name)
			}
			if name == "" {
				name = string(tab.ID)
			}
			name = truncateName(name, 10)
			label := " " + name + " "
			labels = append(labels, label)
			labelWidth := utf8.RuneCountInString(label)
			widths = append(widths, labelWidth)
			totalWidth += labelWidth
			if tab.ID == active {
				activeIndex = i
				foundActive = true
			}
		}
		if !foundActive {
			activeIndex = 0
		}
		if totalWidth <= width {
			windowStart = 0
			start := 0
			end := len(tabs)
			for i := start; i < end; i++ {
				if tabs[i].ID == active {
					b.WriteString(activeStyle)
					b.WriteString(labels[i])
					b.WriteString(barStyle)
				} else {
					b.WriteString(inactiveStyle)
					b.WriteString(labels[i])
					b.WriteString(barStyle)
				}
			}
			line := b.String()
			if visible := visibleWidth(line); visible < width {
				line += strings.Repeat(" ", width-visible)
			}
			line = trimANSIToWidth(line, width)
			return line + ansiReset, windowStart
		}

		window := tabWindowFromStart(widths, windowStart, width)
		if activeIndex < window.start {
			window = tabWindowActiveLeft(widths, activeIndex, width)
		} else if activeIndex >= window.end {
			window = tabWindowActiveRight(widths, activeIndex, width)
		}
		windowStart = window.start

		if window.leftHidden {
			b.WriteString(indicatorStyle)
			b.WriteString("<")
			b.WriteString(barStyle)
		}
		for i := window.start; i < window.end; i++ {
			if tabs[i].ID == active {
				b.WriteString(activeStyle)
				b.WriteString(labels[i])
				b.WriteString(barStyle)
			} else {
				b.WriteString(inactiveStyle)
				b.WriteString(labels[i])
				b.WriteString(barStyle)
			}
		}
		line := b.String()
		if window.rightHidden {
			visible := visibleWidth(line)
			if visible > width-1 {
				line = trimANSIToWidth(line, width-1)
				visible = visibleWidth(line)
			}
			if pad := (width - 1) - visible; pad > 0 {
				line += strings.Repeat(" ", pad)
			}
			line += indicatorStyle + ">" + barStyle
			line = trimANSIToWidth(line, width)
			return line + ansiReset, windowStart
		}
		if visible := visibleWidth(line); visible < width {
			line += strings.Repeat(" ", width-visible)
		}
		line = trimANSIToWidth(line, width)
		return line + ansiReset, windowStart
	}
	line := b.String()
	visible := visibleWidth(line)
	if visible < width {
		line += strings.Repeat(" ", width-visible)
	}
	line = trimANSIToWidth(line, width)
	return line + ansiReset, windowStart
}

type tabWindow struct {
	start       int
	end         int
	leftHidden  bool
	rightHidden bool
}

func tabWindowFromStart(widths []int, start int, width int) tabWindow {
	n := len(widths)
	if n == 0 {
		return tabWindow{}
	}
	if start < 0 {
		start = 0
	}
	if start >= n {
		start = n - 1
	}
	leftHidden := start > 0
	rightHidden := false
	end := start + 1
	for i := 0; i < 3; i++ {
		avail := width
		if leftHidden {
			avail--
		}
		if rightHidden {
			avail--
		}
		if avail < 1 {
			avail = 1
		}
		end = fitForward(widths, start, avail)
		rightHidden = end < n
		leftHidden = start > 0
	}
	return tabWindow{start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden}
}

func tabWindowActiveLeft(widths []int, activeIndex int, width int) tabWindow {
	n := len(widths)
	if n == 0 {
		return tabWindow{}
	}
	if activeIndex < 0 {
		activeIndex = 0
	}
	if activeIndex >= n {
		activeIndex = n - 1
	}
	start := activeIndex
	leftHidden := start > 0
	rightHidden := false
	end := start + 1
	for i := 0; i < 3; i++ {
		avail := width
		if leftHidden {
			avail--
		}
		if rightHidden {
			avail--
		}
		if avail < 1 {
			avail = 1
		}
		end = fitForward(widths, start, avail)
		rightHidden = end < n
		leftHidden = start > 0
	}
	return tabWindow{start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden}
}

func tabWindowActiveRight(widths []int, activeIndex int, width int) tabWindow {
	n := len(widths)
	if n == 0 {
		return tabWindow{}
	}
	if activeIndex < 0 {
		activeIndex = 0
	}
	if activeIndex >= n {
		activeIndex = n - 1
	}
	end := activeIndex + 1
	rightHidden := end < n
	leftHidden := false
	start := end - 1
	for i := 0; i < 3; i++ {
		avail := width
		if leftHidden {
			avail--
		}
		if rightHidden {
			avail--
		}
		if avail < 1 {
			avail = 1
		}
		start = fitBackward(widths, end, avail)
		leftHidden = start > 0
		rightHidden = end < n
	}
	return tabWindow{start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden}
}

func fitForward(widths []int, start int, avail int) int {
	n := len(widths)
	if n == 0 {
		return 0
	}
	if start < 0 {
		start = 0
	}
	if start >= n {
		return n
	}
	if avail < 1 {
		avail = 1
	}
	sum := 0
	end := start
	for i := start; i < n; i++ {
		if sum+widths[i] > avail {
			if i == start {
				return start + 1
			}
			break
		}
		sum += widths[i]
		end = i + 1
	}
	if end == start {
		end = start + 1
	}
	return end
}

func fitBackward(widths []int, end int, avail int) int {
	n := len(widths)
	if n == 0 {
		return 0
	}
	if end < 1 {
		return 0
	}
	if end > n {
		end = n
	}
	if avail < 1 {
		avail = 1
	}
	sum := 0
	start := end
	for i := end - 1; i >= 0; i-- {
		if sum+widths[i] > avail {
			if i == end-1 {
				return end - 1
			}
			break
		}
		sum += widths[i]
		start = i
	}
	if start == end {
		start = end - 1
	}
	return start
}

func renderLine(raw string, width int, theme tuiTheme) string {
	if width <= 0 {
		return ""
	}
	info := classifyLine(raw)
	switch info.kind {
	case lineError:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiBold + ansiFgRGB(theme.ErrorFG) + text + ansiReset
	case lineStderr:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiBold + ansiFgRGB(theme.StderrFG) + text + ansiReset
	case lineMeta:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiDim + ansiItalic + ansiFgRGB(theme.MetaFG) + text + ansiReset
	case lineWorked:
		text := strings.TrimSpace(info.text)
		if text == "" {
			text = "Worked"
		}
		return ansiDim + ansiItalic + ansiFgRGB(theme.MetaFG) + renderWorkedLine(text, width) + ansiReset
	case lineAgent:
		return renderMarkdownLine(info.text, width, markdownStyle{
			codeFG: &theme.CodeFG,
		})
	case lineReasoning:
		return renderMarkdownLine(info.text, width, markdownStyle{
			baseItalic: true,
			baseFG:     &theme.ReasoningFG,
			boldFG:     &theme.ReasoningBold,
			codeFG:     &theme.CodeFG,
		})
	case lineCommand:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiDim + ansiFgRGB(theme.MetaFG) + text + ansiReset
	case lineHelp:
		return renderMarkdownLine(info.text, width, markdownStyle{
			boldFG: &theme.AboutLinkFG,
			codeFG: &theme.HelpArgFG,
		})
	case lineAboutVersion:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiBold + ansiItalic + text + ansiReset
	case lineAboutCopyright:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiFgRGB(theme.AboutCopyrightFG) + text + ansiReset
	case lineAboutLink:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return ansiItalic + ansiFgRGB(theme.AboutLinkFG) + text + ansiReset
	default:
		text := trimToWidth(sanitizeOutputLine(info.text), width)
		if text == "" {
			return text
		}
		return text
	}
}

func renderLines(raw string, width int, theme tuiTheme) []string {
	if width <= 0 {
		return []string{""}
	}
	info := classifyLine(raw)
	switch info.kind {
	case lineAgent:
		return renderMarkdownLines(info.text, width, markdownStyle{
			codeFG: &theme.CodeFG,
		})
	case lineReasoning:
		return renderMarkdownLines(info.text, width, markdownStyle{
			baseItalic: true,
			baseFG:     &theme.ReasoningFG,
			boldFG:     &theme.ReasoningBold,
			codeFG:     &theme.CodeFG,
		})
	case lineError:
		return wrapStyledLines(info.text, width, ansiBold+ansiFgRGB(theme.ErrorFG))
	case lineStderr:
		return wrapStyledLines(info.text, width, ansiBold+ansiFgRGB(theme.StderrFG))
	case lineMeta:
		return wrapStyledLines(info.text, width, ansiDim+ansiItalic+ansiFgRGB(theme.MetaFG))
	case lineCommand:
		return wrapStyledLines(info.text, width, ansiDim+ansiFgRGB(theme.MetaFG))
	case lineHelp:
		return renderMarkdownLines(info.text, width, markdownStyle{
			boldFG: &theme.AboutLinkFG,
			codeFG: &theme.HelpArgFG,
		})
	case lineAboutVersion:
		return wrapStyledLines(info.text, width, ansiBold+ansiItalic)
	case lineAboutCopyright:
		return wrapStyledLines(info.text, width, ansiFgRGB(theme.AboutCopyrightFG))
	case lineAboutLink:
		return wrapStyledLines(info.text, width, ansiItalic+ansiFgRGB(theme.AboutLinkFG))
	case lineWorked:
		return []string{renderLine(raw, width, theme)}
	default:
		return wrapPlainLines(info.text, width)
	}
}

type lineInfo struct {
	text string
	kind lineKind
}

func classifyLine(raw string) lineInfo {
	text := raw
	kind := lineNormal
	if strings.HasPrefix(text, schema.WorkedForMarker) {
		kind = lineWorked
		text = strings.TrimPrefix(text, schema.WorkedForMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.AgentMarker) {
		kind = lineAgent
		text = strings.TrimPrefix(text, schema.AgentMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.ReasoningMarker) {
		kind = lineReasoning
		text = strings.TrimPrefix(text, schema.ReasoningMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.CommandMarker) {
		kind = lineCommand
		text = strings.TrimPrefix(text, schema.CommandMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.HelpMarker) {
		kind = lineHelp
		text = strings.TrimPrefix(text, schema.HelpMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.AboutVersionMarker) {
		kind = lineAboutVersion
		text = strings.TrimPrefix(text, schema.AboutVersionMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.AboutCopyrightMarker) {
		kind = lineAboutCopyright
		text = strings.TrimPrefix(text, schema.AboutCopyrightMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.AboutLinkMarker) {
		kind = lineAboutLink
		text = strings.TrimPrefix(text, schema.AboutLinkMarker)
		return lineInfo{text: text, kind: kind}
	}
	if strings.HasPrefix(text, schema.StderrMarker) {
		kind = lineStderr
		text = strings.TrimPrefix(text, schema.StderrMarker)
	}
	switch {
	case strings.HasPrefix(text, "error:"),
		strings.HasPrefix(text, "command failed:"),
		strings.HasPrefix(text, "command error:"):
		kind = lineError
	case strings.HasPrefix(text, "--- command finished"):
		kind = lineMeta
	}
	return lineInfo{text: text, kind: kind}
}

func renderWorkedLine(label string, width int) string {
	if width <= 0 {
		return ""
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Worked"
	}
	lead := "── " + label + " "
	leadRunes := []rune(lead)
	if len(leadRunes) >= width {
		return trimToWidth(lead, width)
	}
	fill := width - len(leadRunes)
	return lead + strings.Repeat("─", fill)
}

type markdownStyle struct {
	baseItalic bool
	baseBold   bool
	baseFG     *rgb
	boldFG     *rgb
	codeFG     *rgb
}

func renderMarkdownLine(text string, width int, style markdownStyle) string {
	if width <= 0 {
		return ""
	}
	sanitized := sanitizeOutputLine(text)
	spans := markdown.ParseInline(sanitized)
	if len(spans) == 0 {
		return ""
	}
	base := ""
	if style.baseItalic {
		base += ansiItalic
	}
	if style.baseBold {
		base += ansiBold
	}
	if style.baseFG != nil {
		base += ansiFgRGB(*style.baseFG)
	}
	var b strings.Builder
	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		prefix := ansiReset + base
		if span.Code && style.codeFG != nil {
			prefix += ansiFgRGB(*style.codeFG)
		}
		if span.Bold {
			prefix += ansiBold
			if style.boldFG != nil {
				prefix += ansiFgRGB(*style.boldFG)
			}
		}
		if span.Italic {
			prefix += ansiItalic
		}
		b.WriteString(prefix)
		b.WriteString(span.Text)
	}
	out := b.String()
	if out == "" {
		return ""
	}
	out = trimANSIToWidth(out, width)
	return out + ansiReset
}

func renderMarkdownLines(text string, width int, style markdownStyle) []string {
	if width <= 0 {
		return []string{""}
	}
	sanitized := sanitizeOutputLine(text)
	spans := markdown.ParseInline(sanitized)
	if len(spans) == 0 {
		return []string{""}
	}
	base := ""
	if style.baseItalic {
		base += ansiItalic
	}
	if style.baseBold {
		base += ansiBold
	}
	if style.baseFG != nil {
		base += ansiFgRGB(*style.baseFG)
	}

	lines := make([]string, 0, 4)
	var b strings.Builder
	visible := 0
	currentStyle := ""
	suppressLeadingSpace := false

	styleForSpan := func(span markdown.Span) string {
		styleCode := base
		if span.Code && style.codeFG != nil {
			styleCode += ansiFgRGB(*style.codeFG)
		}
		if span.Bold {
			styleCode += ansiBold
			if style.boldFG != nil {
				styleCode += ansiFgRGB(*style.boldFG)
			}
		}
		if span.Italic {
			styleCode += ansiItalic
		}
		return styleCode
	}

	applyStyle := func(styleCode string) {
		if styleCode == currentStyle && b.Len() > 0 {
			return
		}
		if styleCode == "" && b.Len() == 0 {
			currentStyle = ""
			return
		}
		b.WriteString(ansiReset)
		if styleCode != "" {
			b.WriteString(styleCode)
		}
		currentStyle = styleCode
	}

	flushLine := func(wrapped bool) {
		if b.Len() == 0 {
			return
		}
		b.WriteString(ansiReset)
		line := trimANSIToWidth(b.String(), width)
		lines = append(lines, line+ansiReset)
		b.Reset()
		visible = 0
		currentStyle = ""
		suppressLeadingSpace = wrapped
	}

	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		styleCode := styleForSpan(span)
		tokens := tokenizeMarkdown(span)
		for _, token := range tokens {
			if token.text == "" {
				continue
			}
			if token.space {
				if visible == 0 && suppressLeadingSpace {
					continue
				}
				if visible+1 > width {
					flushLine(true)
					continue
				}
				applyStyle(styleCode)
				b.WriteString(" ")
				visible++
				suppressLeadingSpace = false
				continue
			}
			wordRunes := []rune(token.text)
			wordLen := len(wordRunes)
			if wordLen > width {
				if visible > 0 {
					flushLine(true)
				}
				for start := 0; start < wordLen; start += width {
					end := start + width
					if end > wordLen {
						end = wordLen
					}
					applyStyle(styleCode)
					b.WriteString(string(wordRunes[start:end]))
					visible += end - start
					if visible >= width {
						flushLine(true)
					}
				}
				continue
			}
			if visible+wordLen > width && visible > 0 {
				flushLine(true)
			}
			applyStyle(styleCode)
			b.WriteString(token.text)
			visible += wordLen
			suppressLeadingSpace = false
		}
	}
	flushLine(false)
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

type markdownToken struct {
	text  string
	space bool
}

func tokenizeMarkdown(span markdown.Span) []markdownToken {
	if span.Text == "" {
		return nil
	}
	var tokens []markdownToken
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		tokens = append(tokens, markdownToken{text: buf.String()})
		buf.Reset()
	}
	for _, r := range span.Text {
		if unicode.IsSpace(r) {
			flush()
			tokens = append(tokens, markdownToken{text: " ", space: true})
			continue
		}
		buf.WriteRune(r)
	}
	flush()
	return tokens
}

type textToken struct {
	text  string
	space bool
}

func tokenizeText(text string) []textToken {
	if text == "" {
		return nil
	}
	var tokens []textToken
	var buf strings.Builder
	inSpace := false
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		tokens = append(tokens, textToken{text: buf.String(), space: inSpace})
		buf.Reset()
	}
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !inSpace {
				flush()
				inSpace = true
			}
			buf.WriteRune(' ')
			continue
		}
		if inSpace {
			flush()
			inSpace = false
		}
		buf.WriteRune(r)
	}
	flush()
	return tokens
}

func wrapPlainLines(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	sanitized := sanitizeOutputLine(text)
	if sanitized == "" {
		return []string{""}
	}
	tokens := tokenizeText(sanitized)
	lines := make([]string, 0, 4)
	var b strings.Builder
	visible := 0
	suppressLeadingSpace := false
	flush := func(wrapped bool) {
		if b.Len() == 0 {
			return
		}
		lines = append(lines, trimToWidth(b.String(), width))
		b.Reset()
		visible = 0
		suppressLeadingSpace = wrapped
	}
	for _, token := range tokens {
		if token.text == "" {
			continue
		}
		if token.space {
			if visible == 0 && suppressLeadingSpace {
				continue
			}
			spaceLen := len([]rune(token.text))
			if spaceLen <= 0 {
				continue
			}
			if visible+spaceLen > width {
				flush(true)
				continue
			}
			b.WriteString(token.text)
			visible += spaceLen
			continue
		}
		wordRunes := []rune(token.text)
		wordLen := len(wordRunes)
		if wordLen > width {
			if visible > 0 {
				flush(true)
			}
			for start := 0; start < wordLen; start += width {
				end := start + width
				if end > wordLen {
					end = wordLen
				}
				b.WriteString(string(wordRunes[start:end]))
				visible += end - start
				if visible >= width {
					flush(true)
				}
			}
			continue
		}
		if visible+wordLen > width && visible > 0 {
			flush(true)
		}
		b.WriteString(token.text)
		visible += wordLen
		suppressLeadingSpace = false
	}
	flush(false)
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func wrapStyledLines(text string, width int, style string) []string {
	lines := wrapPlainLines(text, width)
	if len(lines) == 1 && lines[0] == "" {
		return lines
	}
	styled := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			styled = append(styled, line)
			continue
		}
		styled = append(styled, style+line+ansiReset)
	}
	return styled
}

func sanitizeOutputLine(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		ch := text[i]
		if ch == 0x1b {
			i = skipEscape(text, i+1)
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if r == '\r' {
			i += size
			continue
		}
		if r == '\t' {
			b.WriteString("    ")
			i += size
			continue
		}
		if r < 0x20 || r == 0x7f {
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func skipEscape(text string, i int) int {
	if i >= len(text) {
		return i
	}
	switch text[i] {
	case '[':
		return skipCSI(text, i+1)
	case ']':
		return skipOSC(text, i+1)
	default:
		if i < len(text) {
			return i + 1
		}
		return i
	}
}

func skipCSI(text string, i int) int {
	for i < len(text) {
		b := text[i]
		if b >= 0x40 && b <= 0x7e {
			return i + 1
		}
		i++
	}
	return i
}

func skipOSC(text string, i int) int {
	for i < len(text) {
		switch text[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 < len(text) && text[i+1] == '\\' {
				return i + 2
			}
		}
		i++
	}
	return i
}

func visibleWidth(text string) int {
	width := 0
	for i := 0; i < len(text); {
		if text[i] == 0x1b {
			i = skipEscape(text, i+1)
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			break
		}
		i += size
		width++
	}
	return width
}

func trimANSIToWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	visible := 0
	for i := 0; i < len(text); {
		if text[i] == 0x1b {
			start := i
			i = skipEscape(text, i+1)
			b.WriteString(text[start:i])
			continue
		}
		if visible >= width {
			break
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			break
		}
		b.WriteRune(r)
		i += size
		visible++
	}
	return b.String()
}
