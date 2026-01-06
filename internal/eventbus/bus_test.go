package eventbus

import (
	"testing"
	"time"

	"pkt.systems/centaurx/schema"
)

func TestSubscribeAndPublish(t *testing.T) {
	bus := New(nil)
	ch, cancel := bus.Subscribe("alice")
	defer cancel()

	event := schema.OutputEvent{UserID: "alice", TabID: "tab1", Lines: []string{"hi"}}
	bus.OnOutput(event)

	select {
	case got := <-ch:
		if got.Type != EventOutput {
			t.Fatalf("expected output event, got %v", got.Type)
		}
		if got.Output.UserID != event.UserID || got.Output.TabID != event.TabID {
			t.Fatalf("unexpected payload: %+v", got.Output)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for event")
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	bus := New(nil)
	ch, cancel := bus.Subscribe("alice")
	cancel()
	if _, ok := <-ch; ok {
		t.Fatalf("expected channel to be closed")
	}
}

func TestPublishDoesNotBlockWhenFull(t *testing.T) {
	bus := New(nil)
	bus.depth = 1
	_, cancel := bus.Subscribe("alice")
	defer cancel()

	var sendCh chan Event
	bus.mu.Lock()
	for ch := range bus.subs["alice"] {
		sendCh = ch
		break
	}
	bus.mu.Unlock()
	if sendCh == nil {
		t.Fatalf("expected subscriber channel")
	}
	sendCh <- Event{Type: EventOutput}
	done := make(chan struct{})
	go func() {
		bus.OnOutput(schema.OutputEvent{UserID: "alice"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("publish blocked on full channel")
	}
}
