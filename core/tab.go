package core

import (
	"context"

	"pkt.systems/centaurx/schema"
)

// tab tracks the state of a single session.
type tab struct {
	ID                   schema.TabID
	Name                 schema.TabName
	Repo                 schema.RepoRef
	Model                schema.ModelID
	ModelReasoningEffort schema.ModelReasoningEffort
	SessionID            schema.SessionID
	Status               schema.TabStatus
	LastUsage            *schema.TurnUsage
	buffer               *buffer
	history              *historyBuffer
	Run                  RunHandle
	RunCancel            context.CancelFunc
	commands             []commandRun
}

type commandRun struct {
	handle CommandHandle
	cancel context.CancelFunc
}

// Snapshot returns a transport-friendly view of the tab.
func (t *tab) Snapshot(active bool, repo schema.RepoRef) schema.TabSnapshot {
	return schema.TabSnapshot{
		ID:                   t.ID,
		Name:                 t.Name,
		Repo:                 repo,
		Model:                t.Model,
		ModelReasoningEffort: t.ModelReasoningEffort,
		SessionID:            t.SessionID,
		Status:               t.Status,
		Active:               active,
	}
}
