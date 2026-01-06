package sshserver

import (
	"fmt"
	"io"
	"strings"
)

type screen struct {
	out io.Writer
}

func newScreen(out io.Writer) *screen {
	return &screen{out: out}
}

func (s *screen) EnterAltScreen() {
	_, _ = io.WriteString(s.out, "\x1b[?1049h\x1b[H\x1b[2J")
}

func (s *screen) ExitAltScreen() {
	_, _ = io.WriteString(s.out, "\x1b[?1049l\x1b[?25h")
}

func (s *screen) Render(lines []string, cursorRow, cursorCol int) error {
	if cursorRow < 1 {
		cursorRow = 1
	}
	if cursorCol < 1 {
		cursorCol = 1
	}
	var b strings.Builder
	b.WriteString("\x1b[?25l")
	b.WriteString("\x1b[H\x1b[2J")
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(line)
	}
	b.WriteString(fmt.Sprintf("\x1b[%d;%dH", cursorRow, cursorCol))
	b.WriteString("\x1b[?25h")
	_, err := io.WriteString(s.out, b.String())
	return err
}
