package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// ErrExists is returned when creating something that already exists.
var ErrExists = errors.New("already exists")

type User struct {
	ID            int64
	Username      string
	PasswordHash  string
	Role          string
	TOTPSecretEnc []byte
	TOTPLastStep  int64
	FailedLogins  int
	LockedUntil   time.Time
	CreatedAt     time.Time
}

const userCols = `id, username, password_hash, role, totp_secret_enc, totp_last_step, failed_logins, locked_until, created_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var locked, created int64
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.TOTPSecretEnc, &u.TOTPLastStep, &u.FailedLogins, &locked, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.LockedUntil, u.CreatedAt = time.Unix(locked, 0), time.Unix(created, 0)
	return u, nil
}

// CreateUser inserts an admin with a password hash and encrypted TOTP secret.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, totpSecretEnc []byte) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, totp_secret_enc, created_at) VALUES (?, ?, 'admin', ?, ?)`,
		username, passwordHash, totpSecretEnc, time.Now().Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("user %s: %w", username, ErrExists)
		}
		return 0, fmt.Errorf("create user %s: %w", username, err)
	}
	return res.LastInsertId()
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// RecordLoginFailure increments the failure counter and, once it reaches
// maxFailures, locks the account until now+lockFor (and resets the counter).
// It reports whether the account is now locked.
func (s *Store) RecordLoginFailure(ctx context.Context, userID int64, maxFailures int, lockFor time.Duration) (bool, error) {
	var locked bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `UPDATE users SET failed_logins = failed_logins + 1 WHERE id = ? RETURNING failed_logins`, userID).Scan(&n); err != nil {
			return err
		}
		if n >= maxFailures {
			locked = true
			_, err := tx.ExecContext(ctx, `UPDATE users SET failed_logins = 0, locked_until = ? WHERE id = ?`, time.Now().Add(lockFor).Unix(), userID)
			return err
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record login failure: %w", err)
	}
	return locked, nil
}

// ResetLoginFailures clears the counter and any lock.
func (s *Store) ResetLoginFailures(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET failed_logins = 0, locked_until = 0 WHERE id = ?`, userID)
	return err
}

// ClaimTOTPStep atomically records step as used. It returns false if this
// step (or a later one) was already used, which blocks replay even when two
// requests with the same code race each other.
func (s *Store) ClaimTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, userID, step)
	if err != nil {
		return false, fmt.Errorf("claim totp step: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetPassword replaces the password hash, clears lockout and logs out all sessions.
func (s *Store) SetPassword(ctx context.Context, userID int64, hash string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, failed_logins = 0, locked_until = 0 WHERE id = ?`, hash, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	})
}

// SetTOTPSecret replaces the TOTP secret, resets the replay step and logs out all sessions.
func (s *Store) SetTOTPSecret(ctx context.Context, userID int64, secretEnc []byte) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret_enc = ?, totp_last_step = 0 WHERE id = ?`, secretEnc, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	})
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
