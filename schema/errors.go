package schema

import "errors"

var (
	// ErrInvalidRequest indicates a malformed request payload.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrInvalidCodexAuth indicates the codex auth payload is invalid.
	ErrInvalidCodexAuth = errors.New("auth.json must be valid JSON")
	// ErrInvalidUser indicates an invalid user identifier.
	ErrInvalidUser = errors.New("invalid user")
	// ErrInvalidRepo indicates an invalid repo identifier.
	ErrInvalidRepo = errors.New("invalid repo")
	// ErrRepoExists indicates a repo already exists.
	ErrRepoExists = errors.New("repo already exists")
	// ErrRepoNotFound indicates a repo could not be found.
	ErrRepoNotFound = errors.New("repo not found")
	// ErrTabNotFound indicates a requested tab could not be found.
	ErrTabNotFound = errors.New("tab not found")
	// ErrNoTabs indicates no tabs exist for the user.
	ErrNoTabs = errors.New("no tabs")
	// ErrInvalidModel indicates an invalid model identifier.
	ErrInvalidModel = errors.New("invalid model")
	// ErrEmptyPrompt indicates the prompt was empty.
	ErrEmptyPrompt = errors.New("empty prompt")
	// ErrRunnerUnavailable indicates no runner is configured.
	ErrRunnerUnavailable = errors.New("runner not configured")
	// ErrTabBusy indicates the tab is already running.
	ErrTabBusy = errors.New("tab is busy")
)
