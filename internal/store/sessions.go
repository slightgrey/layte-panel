package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	TokenHash   string
	UserID      int64
	CSRFToken   string
	TOTPPending bool
	CreatedAt   time.Time
	LastSeenAt  time.Time
	IP          string
	UserAgent   string
	Remember    bool // "remember this browser": longer limits, persistent cookie
	// TOTPSetupEnc is the encrypted 2FA secret being set up in the browser
	// (pending sessions of users without 2FA), until it is confirmed.
	TOTPSetupEnc []byte
}

func (s *Store) CreateSession(ctx context.Context, ss Session) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, totp_pending, created_at, last_seen_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ss.TokenHash, ss.UserID, ss.CSRFToken, ss.TOTPPending, ss.CreatedAt.Unix(), ss.LastSeenAt.Unix(), ss.IP, ss.UserAgent)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) SessionByHash(ctx context.Context, hash string) (Session, error) {
	var ss Session
	var created, seen int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT token_hash, user_id, csrf_token, totp_pending, created_at, last_seen_at, ip, user_agent, remember, totp_setup_enc FROM sessions WHERE token_hash = ?`, hash).
		Scan(&ss.TokenHash, &ss.UserID, &ss.CSRFToken, &ss.TOTPPending, &created, &seen, &ss.IP, &ss.UserAgent, &ss.Remember, &ss.TOTPSetupEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	ss.CreatedAt, ss.LastSeenAt = time.Unix(created, 0), time.Unix(seen, 0)
	return ss, nil
}

func (s *Store) TouchSession(ctx context.Context, hash string, t time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, t.Unix(), hash)
	return err
}

// PromoteSession replaces a pending (pre-2FA) session with a fully
// authenticated one under a new token and CSRF token. Changing the token at
// the privilege change prevents session fixation. remember marks it as a
// "remember this browser" session.
func (s *Store) PromoteSession(ctx context.Context, oldHash, newHash, newCSRF string, now time.Time, remember bool) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET token_hash = ?, csrf_token = ?, totp_pending = 0, totp_setup_enc = NULL, created_at = ?, last_seen_at = ?, remember = ?
		 WHERE token_hash = ? AND totp_pending = 1`,
		newHash, newCSRF, now.Unix(), now.Unix(), remember, oldHash)
	if err != nil {
		return fmt.Errorf("promote session: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// SetSessionTOTPSetup keeps the 2FA secret being set up on a pending session.
func (s *Store) SetSessionTOTPSetup(ctx context.Context, hash string, secretEnc []byte) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET totp_setup_enc = ? WHERE token_hash = ? AND totp_pending = 1`, secretEnc, hash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

// SessionLimits are the cut-off times for one kind of session.
type SessionLimits struct {
	IdleBefore, CreatedBefore time.Time
}

// DeleteExpiredSessions removes normal and remembered sessions past their
// limits, and sessions still pending 2FA since before pendingBefore.
func (s *Store) DeleteExpiredSessions(ctx context.Context, normal, remembered SessionLimits, pendingBefore time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE
		   (totp_pending = 1 AND created_at < ?)
		   OR (remember = 0 AND (last_seen_at < ? OR created_at < ?))
		   OR (remember = 1 AND (last_seen_at < ? OR created_at < ?))`,
		pendingBefore.Unix(), normal.IdleBefore.Unix(), normal.CreatedBefore.Unix(),
		remembered.IdleBefore.Unix(), remembered.CreatedBefore.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}
