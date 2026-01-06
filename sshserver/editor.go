package sshserver

import (
	"bufio"
	"io"
	"unicode"
	"unicode/utf8"
)

type keyKind int

const (
	keyRune keyKind = iota
	keyEnter
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyCtrlA
	keyCtrlE
	keyCtrlW
	keyCtrlD
	keyCtrlC
	keyTab
	keyShiftTab
	keyAltB
	keyAltF
	keyUp
	keyDown
	keyCtrlJ
	keyCtrlU
	keyCtrlK
)

type key struct {
	kind keyKind
	r    rune
}

func readKeys(r io.Reader, out chan<- key) {
	defer close(out)
	br := bufio.NewReader(r)
	lastWasCR := false
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		if lastWasCR {
			lastWasCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case 0x1b:
			readEscape(br, out)
		case '\r':
			out <- key{kind: keyEnter}
			lastWasCR = true
		case '\n':
			out <- key{kind: keyCtrlJ}
		case 0x7f, 0x08:
			out <- key{kind: keyBackspace}
		case 0x01:
			out <- key{kind: keyCtrlA}
		case 0x05:
			out <- key{kind: keyCtrlE}
		case 0x15:
			out <- key{kind: keyCtrlU}
		case 0x0b:
			out <- key{kind: keyCtrlK}
		case 0x17:
			out <- key{kind: keyCtrlW}
		case 0x04:
			out <- key{kind: keyCtrlD}
		case 0x03:
			out <- key{kind: keyCtrlC}
		case 0x09:
			out <- key{kind: keyTab}
		default:
			if b < utf8.RuneSelf {
				out <- key{kind: keyRune, r: rune(b)}
				continue
			}
			_ = br.UnreadByte()
			rn, _, err := br.ReadRune()
			if err != nil {
				return
			}
			out <- key{kind: keyRune, r: rn}
		}
	}
}

func readEscape(br *bufio.Reader, out chan<- key) {
	b, err := br.ReadByte()
	if err != nil {
		return
	}
	switch b {
	case '[':
		readCSI(br, out)
	case 'O':
		readSS3(br, out)
	default:
		switch b {
		case 'b', 'B':
			out <- key{kind: keyAltB}
		case 'f', 'F':
			out <- key{kind: keyAltF}
		}
	}
}

func readCSI(br *bufio.Reader, out chan<- key) {
	seq := []byte{}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		seq = append(seq, b)
		if b == '~' || unicode.IsLetter(rune(b)) {
			break
		}
		if len(seq) > 8 {
			return
		}
	}
	switch string(seq) {
	case "A":
		out <- key{kind: keyUp}
	case "B":
		out <- key{kind: keyDown}
	case "C":
		out <- key{kind: keyRight}
	case "D":
		out <- key{kind: keyLeft}
	case "H":
		out <- key{kind: keyHome}
	case "F":
		out <- key{kind: keyEnd}
	case "5~":
		out <- key{kind: keyPageUp}
	case "6~":
		out <- key{kind: keyPageDown}
	case "3~":
		out <- key{kind: keyDelete}
	case "Z":
		out <- key{kind: keyShiftTab}
	case "1;2Z":
		out <- key{kind: keyShiftTab}
	}
}

func readSS3(br *bufio.Reader, out chan<- key) {
	b, err := br.ReadByte()
	if err != nil {
		return
	}
	switch b {
	case 'H':
		out <- key{kind: keyHome}
	case 'F':
		out <- key{kind: keyEnd}
	}
}

type lineEditor struct {
	buf    []rune
	cursor int
}

func (e *lineEditor) String() string {
	return string(e.buf)
}

func (e *lineEditor) Len() int {
	return len(e.buf)
}

func (e *lineEditor) Clear() {
	e.buf = nil
	e.cursor = 0
}

func (e *lineEditor) SetString(value string) {
	if value == "" {
		e.Clear()
		return
	}
	e.buf = []rune(value)
	e.cursor = len(e.buf)
}

func (e *lineEditor) InsertRune(r rune) {
	if e.cursor < 0 {
		e.cursor = 0
	}
	if e.cursor > len(e.buf) {
		e.cursor = len(e.buf)
	}
	e.buf = append(e.buf[:e.cursor], append([]rune{r}, e.buf[e.cursor:]...)...)
	e.cursor++
}

func (e *lineEditor) Backspace() {
	if e.cursor <= 0 {
		return
	}
	e.buf = append(e.buf[:e.cursor-1], e.buf[e.cursor:]...)
	e.cursor--
}

func (e *lineEditor) Delete() {
	if e.cursor < 0 || e.cursor >= len(e.buf) {
		return
	}
	e.buf = append(e.buf[:e.cursor], e.buf[e.cursor+1:]...)
}

func (e *lineEditor) MoveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *lineEditor) MoveRight() {
	if e.cursor < len(e.buf) {
		e.cursor++
	}
}

func (e *lineEditor) MoveStart() {
	e.cursor = 0
}

func (e *lineEditor) MoveEnd() {
	e.cursor = len(e.buf)
}

func (e *lineEditor) MoveWordLeft() {
	if e.cursor <= 0 {
		return
	}
	i := e.cursor
	for i > 0 && isSpace(e.buf[i-1]) {
		i--
	}
	for i > 0 && !isSpace(e.buf[i-1]) {
		i--
	}
	e.cursor = i
}

func (e *lineEditor) MoveWordRight() {
	if e.cursor >= len(e.buf) {
		return
	}
	i := e.cursor
	for i < len(e.buf) && isSpace(e.buf[i]) {
		i++
	}
	for i < len(e.buf) && !isSpace(e.buf[i]) {
		i++
	}
	e.cursor = i
}

func (e *lineEditor) DeleteWordBackward() {
	if e.cursor <= 0 {
		return
	}
	start := e.cursor
	for start > 0 && isSpace(e.buf[start-1]) {
		start--
	}
	for start > 0 && !isSpace(e.buf[start-1]) {
		start--
	}
	e.buf = append(e.buf[:start], e.buf[e.cursor:]...)
	e.cursor = start
}

func (e *lineEditor) MoveUp() {
	start := e.lineStart()
	if start == 0 {
		return
	}
	col := e.cursor - start
	prevEnd := start - 1
	prevStart := 0
	for i := prevEnd - 1; i >= 0; i-- {
		if e.buf[i] == '\n' {
			prevStart = i + 1
			break
		}
	}
	prevLen := prevEnd - prevStart
	if col > prevLen {
		col = prevLen
	}
	e.cursor = prevStart + col
}

func (e *lineEditor) MoveDown() {
	end := e.lineEnd()
	if end >= len(e.buf) {
		return
	}
	start := e.lineStart()
	col := e.cursor - start
	nextStart := end + 1
	nextEnd := len(e.buf)
	for i := nextStart; i < len(e.buf); i++ {
		if e.buf[i] == '\n' {
			nextEnd = i
			break
		}
	}
	nextLen := nextEnd - nextStart
	if col > nextLen {
		col = nextLen
	}
	e.cursor = nextStart + col
}

func (e *lineEditor) KillLineStart() {
	if e.cursor <= 0 {
		return
	}
	start := e.lineStart()
	if start >= e.cursor {
		return
	}
	e.buf = append(e.buf[:start], e.buf[e.cursor:]...)
	e.cursor = start
}

func (e *lineEditor) KillLineEnd() {
	if e.cursor >= len(e.buf) {
		return
	}
	end := e.lineEnd()
	if end <= e.cursor {
		return
	}
	e.buf = append(e.buf[:e.cursor], e.buf[end:]...)
}

func (e *lineEditor) lineStart() int {
	for i := e.cursor - 1; i >= 0; i-- {
		if e.buf[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

func (e *lineEditor) lineEnd() int {
	for i := e.cursor; i < len(e.buf); i++ {
		if e.buf[i] == '\n' {
			return i
		}
	}
	return len(e.buf)
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}
