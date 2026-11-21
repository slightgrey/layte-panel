package store

import (
	"context"
	"fmt"
	"time"
)

// AuditRow is one audit_log entry. Args must already have secrets redacted.
type AuditRow struct {
	Time     time.Time
	UserID   int64
	Username string
	Action   string
	ArgsJSON string
	OK       bool
	Error    string
}

// InsertAudit appends a row to the audit log.
func (s *Store) InsertAudit(ctx context.Context, r AuditRow) error {
	result := "ok"
	if !r.OK {
		result = "error"
	}
	var uid any
	if r.UserID > 0 {
		uid = r.UserID
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO audit_log (ts, user_id, username, action, args_json, result, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Time.Unix(), uid, r.Username, r.Action, r.ArgsJSON, result, r.Error)
	if err != nil {
		return fmt.Errorf("insert audit %s: %w", r.Action, err)
	}
	return nil
}
