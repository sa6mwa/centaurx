package codex

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

type combinedStream struct {
	events chan schema.ExecEvent
	errMu  sync.Mutex
	err    error
	wg     sync.WaitGroup
	log    pslog.Logger
}

func newCombinedStream(ctx context.Context, stdout io.Reader, stderr io.Reader) *combinedStream {
	stream := &combinedStream{
		events: make(chan schema.ExecEvent, 256),
		log:    pslog.Ctx(ctx),
	}
	stream.wg.Add(2)
	go stream.readJSON(ctx, stdout)
	go stream.readStderr(stderr)
	go func() {
		stream.wg.Wait()
		close(stream.events)
	}()
	return stream
}

func (s *combinedStream) readJSON(ctx context.Context, reader io.Reader) {
	defer s.wg.Done()
	jsonStream := newJSONLStream(reader)
	for {
		event, err := jsonStream.Next(ctx)
		if err != nil {
			var decodeErr *jsonlDecodeError
			if errors.As(err, &decodeErr) {
				line := strings.TrimSpace(string(decodeErr.Line()))
				if line != "" {
					if s.log != nil {
						preview := previewText(line, 200)
						s.log.Warn("codex jsonl decode failed", "preview", preview, "truncated", len(preview) < len(line), "err", err)
					}
					s.events <- schema.ExecEvent{Type: schema.EventError, Message: line}
					continue
				}
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				if s.log != nil {
					s.log.Warn("codex jsonl stream error", "err", err)
				}
				s.setErr(err)
			}
			return
		}
		s.events <- event
	}
}

func (s *combinedStream) readStderr(reader io.Reader) {
	defer s.wg.Done()
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	count := 0
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		count++
		if s.log != nil {
			preview := previewText(text, 200)
			s.log.Trace("codex stderr", "text_len", len(text), "preview", preview, "truncated", len(preview) < len(text))
		}
		s.events <- schema.ExecEvent{Type: schema.EventError, Message: text}
	}
	if err := scanner.Err(); err != nil {
		if s.log != nil {
			s.log.Warn("codex stderr read failed", "err", err)
		}
		s.setErr(err)
	}
	if count > 0 && s.log != nil {
		s.log.Debug("codex stderr completed", "lines", count)
	}
}

func (s *combinedStream) setErr(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *combinedStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	select {
	case <-ctx.Done():
		return schema.ExecEvent{}, ctx.Err()
	case event, ok := <-s.events:
		if ok {
			return event, nil
		}
		s.errMu.Lock()
		err := s.err
		s.errMu.Unlock()
		if err != nil {
			return schema.ExecEvent{}, err
		}
		return schema.ExecEvent{}, io.EOF
	}
}

func (s *combinedStream) Close() error {
	return nil
}

func previewText(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}
