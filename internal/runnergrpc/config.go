package runnergrpc

import "time"

// Config controls the runner gRPC server/client setup.
type Config struct {
	SocketPath        string
	KeepaliveInterval time.Duration
	KeepaliveMisses   int
	CommandNice       int
}
