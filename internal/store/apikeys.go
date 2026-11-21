package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// APIKey is a stored key. The token is never stored, only its hash.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	TokenHash  string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	CreatedBy  int64      `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

const apiKeyCols = `id, name, prefix, token_hash, scopes_json, created_by, created_at, last_used_at, expires_at, revoked_at`

func optTime(unix int64) *time.Time {
	if unix == 0 {
		return nil
	}
	t := time.Unix(unix, 0)
	return &t
}

func scanAPIKey(row interface{ Scan(...any) error }) (APIKey, error) {
	var k APIKey
	var scopes string
	var by sql.NullInt64
	var created, used, expires, revoked int64
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.TokenHash, &scopes, &by, &created, &used, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	json.Unmarshal([]byte(scopes), &k.Scopes)
	k.CreatedBy, k.CreatedAt = by.Int64, time.Unix(created, 0)
	k.LastUsedAt, k.ExpiresAt, k.RevokedAt = optTime(used), optTime(expires), optTime(revoked)
	return k, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, k APIKey) (int64, error) {
	var expires int64
	if k.ExpiresAt != nil {
		expires = k.ExpiresAt.Unix()
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO api_keys (name, prefix, token_hash, scopes_json, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.Name, k.Prefix, k.TokenHash, ToJSON(k.Scopes), nullID(k.CreatedBy), time.Now().Unix(), expires)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) APIKeyByHash(ctx context.Context, hash string) (APIKey, error) {
	return scanAPIKey(s.DB.QueryRowContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE token_hash=?`, hash))
}

func (s *Store) APIKeyByID(ctx context.Context, id int64) (APIKey, error) {
	return scanAPIKey(s.DB.QueryRowContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE id=?`, id))
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys ORDER BY revoked_at != 0, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) RevokeAPIKey(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	return err
}

func (s *Store) TouchAPIKey(ctx context.Context, id int64, t time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=?`, t.Unix(), id)
	return err
}
