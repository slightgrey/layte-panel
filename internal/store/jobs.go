package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// JobRow is a job's persisted state.
type JobRow struct {
	ID         string
	Kind       string
	SiteID     int64
	UserID     int64
	Status     string // running | succeeded | failed
	EventsJSON string
	Error      string
	ResultJSON string
	CreatedAt  time.Time
	FinishedAt time.Time
}

func (s *Store) CreateJob(ctx context.Context, j JobRow) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO jobs (id, kind, site_id, user_id, status, created_at) VALUES (?, ?, ?, ?, 'running', ?)`,
		j.ID, j.Kind, nullID(j.SiteID), nullID(j.UserID), j.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// SaveJobEvents stores the event log of a running job.
func (s *Store) SaveJobEvents(ctx context.Context, id, eventsJSON string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET events_json=? WHERE id=?`, eventsJSON, id)
	return err
}

// FinishJob records the final state.
func (s *Store) FinishJob(ctx context.Context, id, status, eventsJSON, errMsg, resultJSON string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status=?, events_json=?, error=?, result_json=?, finished_at=? WHERE id=?`,
		status, eventsJSON, errMsg, resultJSON, at.Unix(), id)
	return err
}

func (s *Store) JobByID(ctx context.Context, id string) (JobRow, error) {
	var j JobRow
	var siteID, userID sql.NullInt64
	var created, finished int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, kind, site_id, user_id, status, events_json, error, result_json, created_at, finished_at FROM jobs WHERE id=?`, id).
		Scan(&j.ID, &j.Kind, &siteID, &userID, &j.Status, &j.EventsJSON, &j.Error, &j.ResultJSON, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return JobRow{}, ErrNotFound
	}
	if err != nil {
		return JobRow{}, fmt.Errorf("get job: %w", err)
	}
	j.SiteID, j.UserID = siteID.Int64, userID.Int64
	j.CreatedAt = time.Unix(created, 0)
	if finished > 0 {
		j.FinishedAt = time.Unix(finished, 0)
	}
	return j, nil
}

// FailInterruptedJobs marks jobs left "running" by a previous process as failed.
func (s *Store) FailInterruptedJobs(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status='failed', error='interrupted: layte serve restarted while the job was running', finished_at=? WHERE status='running'`,
		time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// ToJSON is a small helper for callers storing values in *_json columns.
func ToJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
