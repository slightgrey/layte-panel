package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type CronJob struct {
	ID        int64     `json:"id"`
	SiteID    int64     `json:"site_id"`
	Schedule  string    `json:"schedule"`
	Command   string    `json:"command"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func scanCron(row interface{ Scan(...any) error }) (CronJob, error) {
	var j CronJob
	var created int64
	err := row.Scan(&j.ID, &j.SiteID, &j.Schedule, &j.Command, &j.Enabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return CronJob{}, ErrNotFound
	}
	j.CreatedAt = time.Unix(created, 0)
	return j, err
}

func (s *Store) AddCronJob(ctx context.Context, j CronJob) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO cron_jobs (site_id, schedule, command, enabled, created_at) VALUES (?, ?, ?, ?, ?)`,
		j.SiteID, j.Schedule, j.Command, j.Enabled, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) CronJobByID(ctx context.Context, id int64) (CronJob, error) {
	return scanCron(s.DB.QueryRowContext(ctx, `SELECT id, site_id, schedule, command, enabled, created_at FROM cron_jobs WHERE id=?`, id))
}

func (s *Store) SetCronEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE cron_jobs SET enabled=? WHERE id=?`, enabled, id)
	return err
}

func (s *Store) DeleteCronJob(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id=?`, id)
	return err
}

// ListCronJobs returns the jobs of one site (siteID > 0) or of all sites.
func (s *Store) ListCronJobs(ctx context.Context, siteID int64) ([]CronJob, error) {
	q := `SELECT id, site_id, schedule, command, enabled, created_at FROM cron_jobs`
	var args []any
	if siteID > 0 {
		q += ` WHERE site_id=?`
		args = append(args, siteID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY site_id, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		j, err := scanCron(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
