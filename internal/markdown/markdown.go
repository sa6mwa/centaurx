package markdown

import "strings"

// Span represents a styled slice of text.
type Span struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
}

// ParseInline parses a subset of inline markdown (bold, italic, code).
// Supported markers: **bold**, *italic*, and `code`.
func ParseInline(input string) []Span {
	if input == "" {
		return nil
	}
	var spans []Span
	var buf strings.Builder
	bold := false
	italic := false
	code := false

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		spans = append(spans, Span{
			Text:   buf.String(),
			Bold:   bold,
			Italic: italic,
			Code:   code,
		})
		buf.Reset()
	}

	for i := 0; i < len(input); {
		ch := input[i]
		if ch == '\\' && i+1 < len(input) {
			buf.WriteByte(input[i+1])
			i += 2
			continue
		}
		if ch == '`' {
			if code {
				flush()
				code = false
				i++
				continue
			}
			if hasClosing(input[i+1:], "`") {
				flush()
				code = true
				i++
				continue
			}
		}
		if !code && ch == '*' {
			if strings.HasPrefix(input[i:], "**") {
				if bold {
					flush()
					bold = false
					i += 2
					continue
				}
				if hasClosing(input[i+2:], "**") {
					flush()
					bold = true
					i += 2
					continue
				}
				buf.WriteString("**")
				i += 2
				continue
			}
			if italic {
				flush()
				italic = false
				i++
				continue
			}
			if hasClosing(input[i+1:], "*") {
				flush()
				italic = true
				i++
				continue
			}
		}
		buf.WriteByte(ch)
		i++
	}
	flush()
	return spans
}

func hasClosing(remaining, marker string) bool {
	if remaining == "" || marker == "" {
		return false
	}
	return strings.Contains(remaining, marker)
}
