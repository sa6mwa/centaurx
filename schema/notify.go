package schema

// OutputEvent represents appended output lines for a tab.
type OutputEvent struct {
	UserID UserID
	TabID  TabID
	Lines  []string
}

// SystemOutputEvent represents output not tied to a tab.
type SystemOutputEvent struct {
	UserID UserID
	Lines  []string
}

// TabEventType describes tab lifecycle or state changes.
type TabEventType string

const (
	// TabEventCreated indicates a tab was created.
	TabEventCreated TabEventType = "created"
	// TabEventClosed indicates a tab was closed.
	TabEventClosed TabEventType = "closed"
	// TabEventActivated indicates a tab became active.
	TabEventActivated TabEventType = "activated"
	// TabEventUpdated indicates a tab was updated.
	TabEventUpdated TabEventType = "updated"
	// TabEventStatus indicates a tab status change.
	TabEventStatus TabEventType = "status"
)

// TabEvent represents a change to a tab or tab list.
type TabEvent struct {
	UserID    UserID
	Type      TabEventType
	Tab       TabSnapshot
	ActiveTab TabID
	Theme     ThemeName
}
