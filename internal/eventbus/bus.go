package eventbus

import (
	"context"
	"sync"

	"pkt.systems/centaurx/schema"
	"pkt.systems/pslog"
)

// EventType identifies the event payload.
type EventType string

const (
	// EventOutput carries output lines for a tab.
	EventOutput EventType = "output"
	// EventSystemOutput carries system output lines.
	EventSystemOutput EventType = "system"
	// EventTab carries tab lifecycle updates.
	EventTab EventType = "tab"
)

// Event represents a UI-facing event emitted by the core service.
type Event struct {
	Type   EventType
	Output schema.OutputEvent
	System schema.SystemOutputEvent
	Tab    schema.TabEvent
}

// Bus fanouts events to per-user subscribers.
type Bus struct {
	mu    sync.Mutex
	subs  map[schema.UserID]map[chan Event]struct{}
	log   pslog.Logger
	depth int
}

// New constructs a Bus.
func New(logger pslog.Logger) *Bus {
	if logger == nil {
		logger = pslog.Ctx(context.Background())
	}
	return &Bus{
		subs:  make(map[schema.UserID]map[chan Event]struct{}),
		log:   logger,
		depth: 256,
	}
}

// Subscribe registers a subscriber for the user and returns a channel + cancel.
func (b *Bus) Subscribe(userID schema.UserID) (<-chan Event, func()) {
	if b == nil {
		return nil, func() {}
	}
	ch := make(chan Event, b.depth)
	b.mu.Lock()
	userSubs := b.subs[userID]
	if userSubs == nil {
		userSubs = make(map[chan Event]struct{})
		b.subs[userID] = userSubs
	}
	userSubs[ch] = struct{}{}
	count := len(userSubs)
	b.mu.Unlock()
	if b.log != nil {
		b.log.With("user", userID).Debug("eventbus subscribe", "subs", count)
	}
	return ch, func() {
		b.mu.Lock()
		if subs := b.subs[userID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subs, userID)
			}
		}
		b.mu.Unlock()
		close(ch)
		if b.log != nil {
			b.log.With("user", userID).Debug("eventbus unsubscribe")
		}
	}
}

// OnOutput publishes an output event.
func (b *Bus) OnOutput(event schema.OutputEvent) {
	b.publish(event.UserID, Event{Type: EventOutput, Output: event})
}

// OnSystemOutput publishes a system output event.
func (b *Bus) OnSystemOutput(event schema.SystemOutputEvent) {
	b.publish(event.UserID, Event{Type: EventSystemOutput, System: event})
}

// OnTabEvent publishes a tab event.
func (b *Bus) OnTabEvent(event schema.TabEvent) {
	b.publish(event.UserID, Event{Type: EventTab, Tab: event})
}

func (b *Bus) publish(userID schema.UserID, event Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	userSubs := b.subs[userID]
	subs := make([]chan Event, 0, len(userSubs))
	for sub := range userSubs {
		subs = append(subs, sub)
	}
	b.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	dropped := 0
	for _, sub := range subs {
		select {
		case sub <- event:
		default:
			dropped++
		}
	}
	if dropped > 0 && b.log != nil {
		b.log.With("user", userID).Trace("eventbus dropped", "count", dropped)
	}
}
