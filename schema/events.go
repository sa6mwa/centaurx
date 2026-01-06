package schema

import "encoding/json"

// EventType is the top-level type emitted by codex exec --json.
type EventType string

const (
	// EventThreadStarted indicates a new thread/session started.
	EventThreadStarted EventType = "thread.started"
	// EventTurnStarted indicates a turn started.
	EventTurnStarted EventType = "turn.started"
	// EventTurnCompleted indicates a turn completed successfully.
	EventTurnCompleted EventType = "turn.completed"
	// EventTurnFailed indicates a turn failed.
	EventTurnFailed EventType = "turn.failed"
	// EventItemStarted indicates an item started.
	EventItemStarted EventType = "item.started"
	// EventItemUpdated indicates an item updated.
	EventItemUpdated EventType = "item.updated"
	// EventItemCompleted indicates an item completed.
	EventItemCompleted EventType = "item.completed"
	// EventError indicates a stream-level error.
	EventError EventType = "error"
)

// ItemType describes the item payload type in item.* events.
type ItemType string

const (
	// ItemAgentMessage represents assistant output.
	ItemAgentMessage ItemType = "agent_message"
	// ItemReasoning represents reasoning content.
	ItemReasoning ItemType = "reasoning"
	// ItemCommandExecution represents a command execution item.
	ItemCommandExecution ItemType = "command_execution"
	// ItemFileChange represents a file change item.
	ItemFileChange ItemType = "file_change"
	// ItemMcpToolCall represents an MCP tool call item.
	ItemMcpToolCall ItemType = "mcp_tool_call"
	// ItemWebSearch represents a web search item.
	ItemWebSearch ItemType = "web_search"
	// ItemTodoList represents a todo list item.
	ItemTodoList ItemType = "todo_list"
	// ItemError represents an error item.
	ItemError ItemType = "error"
)

// ExecEvent is the normalized event shape for codex exec streams.
type ExecEvent struct {
	Type     EventType       `json:"type"`
	ThreadID SessionID       `json:"thread_id,omitempty"`
	Usage    *TurnUsage      `json:"usage,omitempty"`
	Item     *ItemEvent      `json:"item,omitempty"`
	Error    *ErrorEvent     `json:"error,omitempty"`
	Message  string          `json:"message,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// TurnUsage captures token usage reported by codex.
type TurnUsage struct {
	InputTokens       int `json:"input_tokens,omitempty"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens,omitempty"`
}

// ItemEvent captures item payloads from item.* events.
type ItemEvent struct {
	ID               string          `json:"id,omitempty"`
	Type             ItemType        `json:"type,omitempty"`
	Text             string          `json:"text,omitempty"`
	Command          string          `json:"command,omitempty"`
	AggregatedOutput string          `json:"aggregated_output,omitempty"`
	ExitCode         *int            `json:"exit_code,omitempty"`
	Status           string          `json:"status,omitempty"`
	Changes          []FileChange    `json:"changes,omitempty"`
	Query            string          `json:"query,omitempty"`
	Items            []TodoItem      `json:"items,omitempty"`
	Raw              json.RawMessage `json:"-"`
}

// FileChange is a summary of a file change event.
type FileChange struct {
	Path string `json:"path,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// TodoItem is a checklist entry for todo_list items.
type TodoItem struct {
	Text      string `json:"text,omitempty"`
	Completed bool   `json:"completed,omitempty"`
}

// ErrorEvent captures stream-level errors.
type ErrorEvent struct {
	Message string `json:"message,omitempty"`
}
