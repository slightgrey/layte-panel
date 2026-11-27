package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Profile is the part of a user that the user edits on the Account page.
type Profile struct {
	DisplayName string
	Email       string
}

// SetProfile saves a user's display name and email.
func (s *Store) SetProfile(ctx context.Context, userID int64, p Profile) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET display_name = ?, email = ? WHERE id = ?`, p.DisplayName, p.Email, userID)
	return err
}

// ChangePassword replaces the password hash and logs out every other
// session of the user (keepHash, the session making the change, stays).
func (s *Store) ChangePassword(ctx context.Context, userID int64, hash, keepHash string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, failed_logins = 0, locked_until = 0 WHERE id = ?`, hash, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepHash)
		return err
	})
}

// ReplaceTOTP sets a new 2FA secret (step = the code that confirmed it) and
// logs out every other session of the user.
func (s *Store) ReplaceTOTP(ctx context.Context, userID int64, secretEnc []byte, step int64, keepHash string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret_enc = ?, totp_last_step = ? WHERE id = ?`, secretEnc, step, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET totp_setup_enc = NULL WHERE token_hash = ?`, keepHash); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepHash)
		return err
	})
}

// SetSessionTOTPChange keeps a new 2FA secret on a full session until the
// user confirms it with a code (Account page).
func (s *Store) SetSessionTOTPChange(ctx context.Context, hash string, secretEnc []byte) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET totp_setup_enc = ? WHERE token_hash = ? AND totp_pending = 0`, secretEnc, hash)
	return err
}

// SessionsOfUser lists a user's signed-in sessions, newest activity first.
func (s *Store) SessionsOfUser(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT token_hash, created_at, last_seen_at, ip, user_agent, remember FROM sessions
		 WHERE user_id = ? AND totp_pending = 0 ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var ss Session
		var created, seen int64
		if err := rows.Scan(&ss.TokenHash, &created, &seen, &ss.IP, &ss.UserAgent, &ss.Remember); err != nil {
			return nil, err
		}
		ss.UserID, ss.CreatedAt, ss.LastSeenAt = userID, time.Unix(created, 0), time.Unix(seen, 0)
		out = append(out, ss)
	}
	return out, rows.Err()
}

// DeleteOtherSessions logs the user out everywhere except keepHash.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keepHash string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepHash)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
