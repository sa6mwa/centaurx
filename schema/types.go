package schema

// UserID identifies a user in the system.
type UserID string

// TabID identifies a tab session.
type TabID string

// TabName is the user-facing name of a tab.
type TabName string

// SessionID identifies an exec session.
type SessionID string

// RepoName identifies a repository.
type RepoName string

// ModelID identifies an LLM model.
type ModelID string

// ThemeName identifies a UI theme.
type ThemeName string

// RepoRef identifies a repository available to the runner.
type RepoRef struct {
	Name RepoName
	Path string
}
