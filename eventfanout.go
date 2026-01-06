package centaurx

import (
	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/schema"
)

type eventFanout struct {
	sinks []core.EventSink
}

func (f eventFanout) OnOutput(event schema.OutputEvent) {
	for _, sink := range f.sinks {
		if sink == nil {
			continue
		}
		sink.OnOutput(event)
	}
}

func (f eventFanout) OnSystemOutput(event schema.SystemOutputEvent) {
	for _, sink := range f.sinks {
		if sink == nil {
			continue
		}
		sink.OnSystemOutput(event)
	}
}

func (f eventFanout) OnTabEvent(event schema.TabEvent) {
	for _, sink := range f.sinks {
		if sink == nil {
			continue
		}
		sink.OnTabEvent(event)
	}
}
