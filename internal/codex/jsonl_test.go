package codex

import (
	"bytes"
	"context"
	"io"
	"testing"

	"pkt.systems/centaurx/schema"
)

func TestDecodeEventPreservesRaw(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"it1","type":"agent_message","text":"hello"}}`)
	event, err := decodeEvent(line)
	if err != nil {
		t.Fatalf("decodeEvent: %v", err)
	}
	if event.Type != schema.EventItemCompleted {
		t.Fatalf("unexpected event type: %s", event.Type)
	}
	if len(event.Raw) == 0 {
		t.Fatalf("expected raw event")
	}
	if event.Item == nil {
		t.Fatalf("expected item payload")
	}
	if len(event.Item.Raw) == 0 {
		t.Fatalf("expected raw item")
	}
	if event.Item.Text != "hello" {
		t.Fatalf("unexpected item text: %q", event.Item.Text)
	}
}

func TestJSONLStreamReadsEvents(t *testing.T) {
	data := []byte("\n" +
		`{"type":"thread.started","thread_id":"t1"}` + "\n" +
		`{"type":"item.completed","item":{"id":"it1","type":"agent_message","text":"hi"}}` + "\n")
	stream := newJSONLStream(bytes.NewReader(data))

	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.Type != schema.EventThreadStarted || event.ThreadID != "t1" {
		t.Fatalf("unexpected first event: %+v", event)
	}

	event, err = stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(2): %v", err)
	}
	if event.Type != schema.EventItemCompleted || event.Item == nil {
		t.Fatalf("unexpected second event: %+v", event)
	}

	_, err = stream.Next(context.Background())
	if err == io.EOF {
		return
	}
	if err == nil {
		t.Fatalf("expected EOF, got nil")
	}
}
