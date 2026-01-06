package core

import (
	"context"

	"pkt.systems/centaurx/schema"
)

// Service is the transport-agnostic API for managing tabs, repos, and codex exec sessions.
type Service interface {
	CreateTab(ctx context.Context, req schema.CreateTabRequest) (schema.CreateTabResponse, error)
	CloseTab(ctx context.Context, req schema.CloseTabRequest) (schema.CloseTabResponse, error)
	ListTabs(ctx context.Context, req schema.ListTabsRequest) (schema.ListTabsResponse, error)
	ActivateTab(ctx context.Context, req schema.ActivateTabRequest) (schema.ActivateTabResponse, error)
	SendPrompt(ctx context.Context, req schema.SendPromptRequest) (schema.SendPromptResponse, error)
	SetModel(ctx context.Context, req schema.SetModelRequest) (schema.SetModelResponse, error)
	SwitchRepo(ctx context.Context, req schema.SwitchRepoRequest) (schema.SwitchRepoResponse, error)
	ListRepos(ctx context.Context, req schema.ListReposRequest) (schema.ListReposResponse, error)
	StopSession(ctx context.Context, req schema.StopSessionRequest) (schema.StopSessionResponse, error)
	RenewSession(ctx context.Context, req schema.RenewSessionRequest) (schema.RenewSessionResponse, error)
	SetTheme(ctx context.Context, req schema.SetThemeRequest) (schema.SetThemeResponse, error)
	GetBuffer(ctx context.Context, req schema.GetBufferRequest) (schema.GetBufferResponse, error)
	ScrollBuffer(ctx context.Context, req schema.ScrollBufferRequest) (schema.ScrollBufferResponse, error)
	AppendOutput(ctx context.Context, req schema.AppendOutputRequest) (schema.AppendOutputResponse, error)
	AppendSystemOutput(ctx context.Context, req schema.AppendSystemOutputRequest) (schema.AppendSystemOutputResponse, error)
	GetSystemBuffer(ctx context.Context, req schema.GetSystemBufferRequest) (schema.GetSystemBufferResponse, error)
	GetHistory(ctx context.Context, req schema.GetHistoryRequest) (schema.GetHistoryResponse, error)
	AppendHistory(ctx context.Context, req schema.AppendHistoryRequest) (schema.AppendHistoryResponse, error)
	SaveCodexAuth(ctx context.Context, req schema.SaveCodexAuthRequest) (schema.SaveCodexAuthResponse, error)
	GetTabUsage(ctx context.Context, req schema.GetTabUsageRequest) (schema.GetTabUsageResponse, error)
}

// CommandTracker allows tracking long-running shell commands per tab.
type CommandTracker interface {
	RegisterCommand(ctx context.Context, userID schema.UserID, tabID schema.TabID, handle CommandHandle, cancel context.CancelFunc)
	UnregisterCommand(userID schema.UserID, tabID schema.TabID, handle CommandHandle)
}
