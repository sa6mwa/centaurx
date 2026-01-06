package httpapi

import (
	"context"
	"sync"
	"time"

	"pkt.systems/centaurx/internal/logx"
	"pkt.systems/centaurx/schema"
)

// StreamEvent is sent to SSE clients.
type StreamEvent struct {
	Seq       uint64              `json:"seq"`
	Type      string              `json:"type"`
	TabEvent  string              `json:"tab_event,omitempty"`
	TabID     schema.TabID        `json:"tab_id,omitempty"`
	Lines     []string            `json:"lines,omitempty"`
	Tab       *schema.TabSnapshot `json:"tab,omitempty"`
	ActiveTab schema.TabID        `json:"active_tab,omitempty"`
	Theme     schema.ThemeName    `json:"theme,omitempty"`
	Snapshot  *SnapshotPayload    `json:"snapshot,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

// SnapshotPayload seeds client state on connect.
type SnapshotPayload struct {
	Tabs      []schema.TabSnapshot                   `json:"tabs"`
	ActiveTab schema.TabID                           `json:"active_tab"`
	Buffers   map[schema.TabID]schema.BufferSnapshot `json:"buffers"`
	System    schema.SystemBufferSnapshot            `json:"system"`
	Theme     schema.ThemeName                       `json:"theme,omitempty"`
}

// Hub broadcasts events per user.
type Hub struct {
	mu          sync.Mutex
	users       map[schema.UserID]*userHub
	historySize int
}

// NewHub constructs a hub with the given history size.
func NewHub(historySize int) *Hub {
	if historySize <= 0 {
		historySize = 1000
	}
	return &Hub{
		users:       make(map[schema.UserID]*userHub),
		historySize: historySize,
	}
}

// OnOutput implements core.EventSink.
func (h *Hub) OnOutput(event schema.OutputEvent) {
	log := logx.WithUser(context.Background(), event.UserID).With("tab", event.TabID)
	log.Trace("hub output event", "lines", len(event.Lines))
	h.publish(event.UserID, StreamEvent{
		Type:      "output",
		TabID:     event.TabID,
		Lines:     event.Lines,
		Timestamp: time.Now(),
	})
}

// OnSystemOutput implements core.EventSink.
func (h *Hub) OnSystemOutput(event schema.SystemOutputEvent) {
	log := logx.WithUser(context.Background(), event.UserID)
	log.Trace("hub system event", "lines", len(event.Lines))
	h.publish(event.UserID, StreamEvent{
		Type:      "system",
		Lines:     event.Lines,
		Timestamp: time.Now(),
	})
}

// OnTabEvent implements core.EventSink.
func (h *Hub) OnTabEvent(event schema.TabEvent) {
	log := logx.WithUser(context.Background(), event.UserID)
	log.Trace("hub tab event", "type", event.Type, "tab", event.Tab.ID, "active", event.ActiveTab)
	tab := event.Tab
	h.publish(event.UserID, StreamEvent{
		Type:      "tab",
		TabEvent:  string(event.Type),
		Tab:       &tab,
		Theme:     event.Theme,
		Timestamp: time.Now(),
	})
}

// Subscribe registers a subscriber for a user.
func (h *Hub) Subscribe(userID schema.UserID) (<-chan StreamEvent, func(), uint64, []StreamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	uh := h.getOrCreateUserHubLocked(userID)
	ch := make(chan StreamEvent, 256)
	uh.subs[ch] = struct{}{}
	history := append([]StreamEvent(nil), uh.history...)
	seq := uh.seq
	log := logx.WithUser(context.Background(), userID)
	log.Info("hub subscribe", "subs", len(uh.subs), "history", len(history))
	unsub := func() {
		h.mu.Lock()
		delete(uh.subs, ch)
		close(ch)
		remaining := len(uh.subs)
		h.mu.Unlock()
		log.Info("hub unsubscribe", "subs", remaining)
	}
	return ch, unsub, seq, history
}

// Replay returns events after the provided seq.
func (h *Hub) Replay(userID schema.UserID, after uint64) []StreamEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	uh := h.users[userID]
	if uh == nil {
		return nil
	}
	events := make([]StreamEvent, 0, len(uh.history))
	for _, event := range uh.history {
		if event.Seq > after {
			events = append(events, event)
		}
	}
	logx.WithUser(context.Background(), userID).Debug("hub replay", "after", after, "count", len(events))
	return events
}

func (h *Hub) publish(userID schema.UserID, event StreamEvent) {
	h.mu.Lock()
	uh := h.getOrCreateUserHubLocked(userID)
	uh.seq++
	event.Seq = uh.seq
	uh.history = append(uh.history, event)
	if len(uh.history) > h.historySize {
		uh.history = uh.history[len(uh.history)-h.historySize:]
	}
	subs := make([]chan StreamEvent, 0, len(uh.subs))
	for sub := range uh.subs {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	dropped := 0
	for _, sub := range subs {
		select {
		case sub <- event:
		default:
			dropped++
		}
	}
	if dropped > 0 {
		logx.WithUser(context.Background(), userID).Warn("hub event dropped", "type", event.Type, "dropped", dropped)
	}
}

func (h *Hub) getOrCreateUserHubLocked(userID schema.UserID) *userHub {
	uh := h.users[userID]
	if uh == nil {
		uh = &userHub{
			subs: make(map[chan StreamEvent]struct{}),
		}
		h.users[userID] = uh
	}
	return uh
}

type userHub struct {
	seq     uint64
	history []StreamEvent
	subs    map[chan StreamEvent]struct{}
}
