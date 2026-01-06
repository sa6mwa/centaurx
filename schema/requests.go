package schema

// Tab lifecycle.

// CreateTabRequest describes a request to create a tab.
type CreateTabRequest struct {
	UserID     UserID
	RepoName   RepoName
	RepoURL    string
	CreateRepo bool
	TabName    TabName
}

// CreateTabResponse reports the created tab and repo status.
type CreateTabResponse struct {
	Tab         TabSnapshot
	RepoCreated bool
}

// CloseTabRequest describes a request to close a tab.
type CloseTabRequest struct {
	UserID UserID
	TabID  TabID
}

// CloseTabResponse reports the closed tab snapshot.
type CloseTabResponse struct {
	Tab TabSnapshot
}

// ListTabsRequest describes a request to list tabs.
type ListTabsRequest struct {
	UserID UserID
}

// ListTabsResponse reports tabs and active context.
type ListTabsResponse struct {
	Tabs       []TabSnapshot
	ActiveTab  TabID
	ActiveRepo RepoRef
	Theme      ThemeName
}

// ActivateTabRequest describes a request to activate a tab.
type ActivateTabRequest struct {
	UserID UserID
	TabID  TabID
}

// ActivateTabResponse reports the activated tab snapshot.
type ActivateTabResponse struct {
	Tab TabSnapshot
}

// Repo operations.

// SwitchRepoRequest describes a request to switch repos.
type SwitchRepoRequest struct {
	UserID   UserID
	TabID    TabID
	RepoName RepoName
}

// SwitchRepoResponse reports the updated tab snapshot.
type SwitchRepoResponse struct {
	Tab TabSnapshot
}

// ListReposRequest describes a request to list repos.
type ListReposRequest struct {
	UserID UserID
}

// ListReposResponse reports available repos.
type ListReposResponse struct {
	Repos []RepoRef
}

// Prompt and schema.

// SendPromptRequest describes a prompt submission.
type SendPromptRequest struct {
	UserID UserID
	TabID  TabID
	Prompt string
}

// SendPromptResponse reports prompt acceptance and tab state.
type SendPromptResponse struct {
	Tab      TabSnapshot
	Accepted bool
}

// SetModelRequest describes a request to set the model for a tab.
type SetModelRequest struct {
	UserID UserID
	TabID  TabID
	Model  ModelID
}

// SetModelResponse reports the updated tab snapshot.
type SetModelResponse struct {
	Tab TabSnapshot
}

// SetThemeRequest describes a request to set the UI theme.
type SetThemeRequest struct {
	UserID UserID
	Theme  ThemeName
}

// SetThemeResponse reports the applied theme.
type SetThemeResponse struct {
	Theme ThemeName
}

// Stop session.

// StopSessionRequest describes a request to stop a running session.
type StopSessionRequest struct {
	UserID UserID
	TabID  TabID
}

// StopSessionResponse reports the updated tab snapshot.
type StopSessionResponse struct {
	Tab TabSnapshot
}

// RenewSessionRequest describes a request to reset a tab's exec session.
type RenewSessionRequest struct {
	UserID UserID
	TabID  TabID
}

// RenewSessionResponse reports the updated tab snapshot.
type RenewSessionResponse struct {
	Tab TabSnapshot
}

// Buffer view and scrolling.

// GetBufferRequest describes a request to fetch buffer lines.
type GetBufferRequest struct {
	UserID UserID
	TabID  TabID
	Limit  int
}

// GetBufferResponse reports the buffer snapshot.
type GetBufferResponse struct {
	Buffer BufferSnapshot
}

// ScrollBufferRequest describes a request to scroll the buffer.
type ScrollBufferRequest struct {
	UserID UserID
	TabID  TabID
	Delta  int
	Limit  int
}

// ScrollBufferResponse reports the buffer snapshot after scrolling.
type ScrollBufferResponse struct {
	Buffer BufferSnapshot
}

// Output append.

// AppendOutputRequest describes a request to append output lines to a tab.
type AppendOutputRequest struct {
	UserID UserID
	TabID  TabID
	Lines  []string
}

// AppendOutputResponse reports the updated tab snapshot.
type AppendOutputResponse struct {
	Tab TabSnapshot
}

// System output.

// AppendSystemOutputRequest describes a request to append system output lines.
type AppendSystemOutputRequest struct {
	UserID UserID
	Lines  []string
}

// AppendSystemOutputResponse reports completion of the append.
type AppendSystemOutputResponse struct{}

// GetSystemBufferRequest describes a request to fetch system buffer lines.
type GetSystemBufferRequest struct {
	UserID UserID
	Limit  int
}

// GetSystemBufferResponse reports the system buffer snapshot.
type GetSystemBufferResponse struct {
	Buffer SystemBufferSnapshot
}

// History.

// GetHistoryRequest describes a request to fetch prompt history.
type GetHistoryRequest struct {
	UserID UserID
	TabID  TabID
}

// GetHistoryResponse reports the prompt history.
type GetHistoryResponse struct {
	Entries []string
}

// AppendHistoryRequest describes a request to append a history entry.
type AppendHistoryRequest struct {
	UserID UserID
	TabID  TabID
	Entry  string
}

// AppendHistoryResponse reports the updated history.
type AppendHistoryResponse struct {
	Entries []string
}

// Codex auth.

// SaveCodexAuthRequest describes a request to save codex auth.json contents.
type SaveCodexAuthRequest struct {
	UserID   UserID
	AuthJSON []byte
}

// SaveCodexAuthResponse reports completion of the write.
type SaveCodexAuthResponse struct{}

// Tab usage.

// GetTabUsageRequest describes a request to fetch latest usage for a tab.
type GetTabUsageRequest struct {
	UserID UserID
	TabID  TabID
}

// GetTabUsageResponse reports the last observed usage for a tab.
type GetTabUsageResponse struct {
	Usage *TurnUsage
}
