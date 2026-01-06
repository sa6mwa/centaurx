package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"

	"pkt.systems/centaurx/schema"
)

type jsonlStream struct {
	reader *bufio.Reader
	closed bool
}

type jsonlDecodeError struct {
	line []byte
	err  error
}

func (e *jsonlDecodeError) Error() string {
	if e == nil || e.err == nil {
		return "jsonl decode error"
	}
	return e.err.Error()
}

func (e *jsonlDecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *jsonlDecodeError) Line() []byte {
	if e == nil {
		return nil
	}
	return e.line
}

func newJSONLStream(r io.Reader) *jsonlStream {
	return &jsonlStream{reader: bufio.NewReader(r)}
}

func (s *jsonlStream) Next(ctx context.Context) (schema.ExecEvent, error) {
	for {
		if ctx.Err() != nil {
			return schema.ExecEvent{}, ctx.Err()
		}
		line, err := s.reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return schema.ExecEvent{}, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err != nil {
				return schema.ExecEvent{}, err
			}
			continue
		}
		event, decodeErr := decodeEvent(line)
		if decodeErr != nil {
			return schema.ExecEvent{}, &jsonlDecodeError{line: append([]byte(nil), line...), err: decodeErr}
		}
		return event, nil
	}
}

func (s *jsonlStream) Close() error {
	s.closed = true
	return nil
}

func decodeEvent(line []byte) (schema.ExecEvent, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return schema.ExecEvent{}, err
	}
	var event schema.ExecEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return schema.ExecEvent{}, err
	}
	event.Raw = append([]byte(nil), line...)
	if itemRaw, ok := raw["item"]; ok && event.Item != nil {
		event.Item.Raw = itemRaw
	}
	return event, nil
}
