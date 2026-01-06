package core

import "pkt.systems/centaurx/schema"

// Renderer formats normalized events into display lines for a transport.
type Renderer interface {
	FormatEvent(event schema.ExecEvent) ([]string, error)
}
