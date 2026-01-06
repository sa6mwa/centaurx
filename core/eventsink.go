package core

import "pkt.systems/centaurx/schema"

// EventSink receives tab and output events from the core service.
type EventSink interface {
	OnOutput(event schema.OutputEvent)
	OnSystemOutput(event schema.SystemOutputEvent)
	OnTabEvent(event schema.TabEvent)
}
