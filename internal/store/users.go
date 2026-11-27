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
	// SiteID is the main site of a site account (Role "site"); 0 for admins.
	SiteID        int64
	MaxSubdomains int
	DisplayName   string // Account page; "" = show the username
	Email         string
	// SetupExpires is when the user's pending setup link expires (zero = none).
	SetupExpires time.Time
	// Limits of a site login (0 = none).
	Limits Limits
}

// Limits are a site login's resource limits; 0 means no limit.
type Limits struct {
	CPUPercent int `json:"cpu_percent"` // of one CPU (200 = two CPUs)
	MemoryMB   int `json:"memory_mb"`
	DiskMB     int `json:"disk_mb"`
}

// Roles.
const (
	RoleAdmin = "admin"
	RoleSite  = "site"
)

// IsAdmin reports whether u is a full admin.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

const userCols = `id, username, password_hash, role, totp_secret_enc, totp_last_step, failed_logins, locked_until, created_at, site_id, max_subdomains, display_name, email, setup_expires, cpu_percent, memory_mb, disk_mb`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var locked, created int64
	var site sql.NullInt64
	var setupExp int64
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.TOTPSecretEnc, &u.TOTPLastStep, &u.FailedLogins, &locked, &created, &site, &u.MaxSubdomains, &u.DisplayName, &u.Email, &setupExp, &u.Limits.CPUPercent, &u.Limits.MemoryMB, &u.Limits.DiskMB)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.LockedUntil, u.CreatedAt = time.Unix(locked, 0), time.Unix(created, 0)
	u.SiteID = site.Int64
	if setupExp > 0 {
		u.SetupExpires = time.Unix(setupExp, 0)
	}
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

// CreateSiteUser inserts a site account for the main site siteID. 2FA is
// set up by the user at the first login (totp_secret_enc stays NULL).
func (s *Store) CreateSiteUser(ctx context.Context, username, passwordHash string, siteID int64, maxSubdomains int) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, site_id, max_subdomains, created_at) VALUES (?, ?, 'site', ?, ?, ?)`,
		username, passwordHash, siteID, maxSubdomains, time.Now().Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("user %s: %w", username, ErrExists)
		}
		return 0, fmt.Errorf("create user %s: %w", username, err)
	}
	return res.LastInsertId()
}

// ListUsers returns every panel user, admins first.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY role = 'site', username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserBySite returns the site account of a main site.
func (s *Store) UserBySite(ctx context.Context, siteID int64) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE site_id = ?`, siteID))
}

// DeleteUser removes a user (their sessions go with it).
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// SetMaxSubdomains changes a site account's subdomain limit.
func (s *Store) SetMaxSubdomains(ctx context.Context, id int64, n int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET max_subdomains = ? WHERE id = ?`, n, id)
	return err
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

// EnrollTOTP stores a 2FA secret confirmed in the browser and records step
// as used. It only succeeds while the user has no secret, so it can't
// replace one that is already set up. Sessions are kept (the caller promotes
// the one that did the setup).
func (s *Store) EnrollTOTP(ctx context.Context, userID int64, secretEnc []byte, step int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET totp_secret_enc = ?, totp_last_step = ? WHERE id = ? AND totp_secret_enc IS NULL`, secretEnc, step, userID)
	if err != nil {
		return false, fmt.Errorf("enroll totp: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ClearTOTP removes the 2FA secret, so the user sets it up again at the next
// login, and logs out all sessions.
func (s *Store) ClearTOTP(ctx context.Context, userID int64) error {
	return s.SetTOTPSecret(ctx, userID, nil)
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

// SetSetupToken gives the user a new setup link (replacing any older one).
func (s *Store) SetSetupToken(ctx context.Context, userID int64, tokenHash string, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET setup_token_hash = ?, setup_expires = ? WHERE id = ?`, tokenHash, expires.Unix(), userID)
	return err
}

// UserBySetupToken returns the user whose setup link is tokenHash, if it
// hasn't expired at now.
func (s *Store) UserBySetupToken(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	if tokenHash == "" {
		return User{}, ErrNotFound
	}
	return scanUser(s.DB.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE setup_token_hash = ? AND setup_expires > ?`, tokenHash, now.Unix()))
}

// UseSetupToken sets the password chosen through a setup link, uses up the
// link (only if it is still tokenHash, so it works once) and logs out every
// session. It reports whether the link was still valid.
func (s *Store) UseSetupToken(ctx context.Context, userID int64, tokenHash, passwordHash string) (bool, error) {
	var ok bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE users SET password_hash = ?, setup_token_hash = '', setup_expires = 0, failed_logins = 0, locked_until = 0
			 WHERE id = ? AND setup_token_hash = ? AND setup_token_hash <> ''`, passwordHash, userID, tokenHash)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil
		}
		ok = true
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	})
	return ok, err
}

// SetLimits saves a site login's resource limits.
func (s *Store) SetLimits(ctx context.Context, userID int64, l Limits) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET cpu_percent = ?, memory_mb = ?, disk_mb = ? WHERE id = ?`, l.CPUPercent, l.MemoryMB, l.DiskMB, userID)
	return err
}
