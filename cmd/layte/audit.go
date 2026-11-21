package main

import (
	"context"

	"layte-panel/internal/agent"
	"layte-panel/internal/store"
)

// storeAuditor adapts *store.Store to the agent.Auditor interface, so neither
// package has to import the other.
type storeAuditor struct{ st *store.Store }

func (a storeAuditor) Audit(ctx context.Context, e agent.AuditEntry) error {
	return a.st.InsertAudit(ctx, store.AuditRow{
		Time:     e.Time,
		UserID:   e.Actor.UserID,
		Username: e.Actor.Username,
		Action:   e.Action,
		ArgsJSON: e.ArgsJSON,
		OK:       e.OK,
		Error:    e.Error,
	})
}
