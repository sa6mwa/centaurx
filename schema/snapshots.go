package schema

// TabStatus describes the current state of a tab session.
type TabStatus string

const (
	// TabStatusIdle indicates a tab is idle.
	TabStatusIdle TabStatus = "idle"
	// TabStatusRunning indicates a tab is processing a prompt or command.
	TabStatusRunning TabStatus = "running"
	// TabStatusStopped indicates a tab has been stopped.
	TabStatusStopped TabStatus = "stopped"
)

// TabSnapshot is a read-only view of tab state for transports.
type TabSnapshot struct {
	ID        TabID
	Name      TabName
	Repo      RepoRef
	Model     ModelID
	SessionID SessionID
	Status    TabStatus
	Active    bool
}

// BufferSnapshot represents the current scrollback view.
type BufferSnapshot struct {
	TabID        TabID
	Lines        []string
	TotalLines   int
	ScrollOffset int
	AtBottom     bool
}

// SystemBufferSnapshot represents output not tied to a tab.
type SystemBufferSnapshot struct {
	Lines        []string
	TotalLines   int
	ScrollOffset int
	AtBottom     bool
}
