package core

import (
	"context"

	"pkt.systems/centaurx/schema"
)

// tab tracks the state of a single session.
type tab struct {
	ID        schema.TabID
	Name      schema.TabName
	Repo      schema.RepoRef
	Model     schema.ModelID
	SessionID schema.SessionID
	Status    schema.TabStatus
	LastUsage *schema.TurnUsage
	buffer    *buffer
	history   *historyBuffer
	Run       RunHandle
	RunCancel context.CancelFunc
	commands  []commandRun
}

type commandRun struct {
	handle CommandHandle
	cancel context.CancelFunc
}

// Snapshot returns a transport-friendly view of the tab.
func (t *tab) Snapshot(active bool) schema.TabSnapshot {
	return schema.TabSnapshot{
		ID:        t.ID,
		Name:      t.Name,
		Repo:      t.Repo,
		Model:     t.Model,
		SessionID: t.SessionID,
		Status:    t.Status,
		Active:    active,
	}
}
