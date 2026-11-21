package store

import (
	"context"
	"path/filepath"
	"testing"
)

// openTest opens a fresh database in a temp dir; it is closed when the test ends.
func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ { // second open must not re-apply migrations
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n < 1 {
			t.Errorf("expected migrations recorded, got %d", n)
		}
		s.Close()
	}
}

func TestForeignKeysOn(t *testing.T) {
	s := openTest(t)
	var on int
	if err := s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Errorf("foreign_keys = %d, want 1", on)
	}
}
