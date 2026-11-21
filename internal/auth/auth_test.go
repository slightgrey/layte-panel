package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash format: %s", h)
	}
	tests := []struct {
		pw   string
		want bool
	}{
		{"correct horse battery staple", true},
		{"correct horse battery stapl", false},
		{"", false},
	}
	for _, tt := range tests {
		ok, err := VerifyPassword(tt.pw, h)
		if err != nil || ok != tt.want {
			t.Errorf("VerifyPassword(%q) = %v, %v; want %v", tt.pw, ok, err, tt.want)
		}
	}
	h2, _ := HashPassword("correct horse battery staple")
	if h == h2 {
		t.Error("two hashes of the same password must differ (random salt)")
	}
	if _, err := VerifyPassword("x", "$bcrypt$nope"); err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestValidateUsername(t *testing.T) {
	tests := map[string]bool{
		"admin": true, "a_b-c9": true, "ab": false, "Admin": false, "9admin": false,
		"ad min": false, "../etc": false, strings.Repeat("a", 33): false, strings.Repeat("a", 32): true,
	}
	for u, ok := range tests {
		if err := ValidateUsername(u); (err == nil) != ok {
			t.Errorf("ValidateUsername(%q) = %v, want ok=%v", u, err, ok)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("short password accepted")
	}
	if ValidatePassword(strings.Repeat("x", MaxPasswordLen+1)) == nil {
		t.Error("overlong password accepted")
	}
	if err := ValidatePassword("twelve chars"); err != nil {
		t.Error(err)
	}
}

func TestCheckTOTP(t *testing.T) {
	enr, err := NewTOTP("admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	step := now.Unix() / 30
	code := func(at time.Time) string { c, _ := TOTPCode(enr.Secret, at); return c }

	tests := []struct {
		name     string
		code     string
		lastStep int64
		wantOK   bool
	}{
		{"current", code(now), 0, true},
		{"previous window (skew)", code(now.Add(-30 * time.Second)), 0, true},
		{"next window (skew)", code(now.Add(30 * time.Second)), 0, true},
		{"too old", code(now.Add(-90 * time.Second)), 0, false},
		{"replay of used step", code(now), step, false},
		{"older than last used", code(now.Add(-30 * time.Second)), step, false},
		{"wrong length", "12345", 0, false},
		{"garbage", "abcdef", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := CheckTOTP(enr.Secret, tt.code, now, tt.lastStep); ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestKeyring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secret.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := k1.Seal([]byte("JBSWY3DPEHPK3PXP"), []byte("admin"))
	if err != nil {
		t.Fatal(err)
	}
	// Reloading the same file must decrypt.
	k2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k2.Open(ct, []byte("admin"))
	if err != nil || !bytes.Equal(pt, []byte("JBSWY3DPEHPK3PXP")) {
		t.Fatalf("Open = %q, %v", pt, err)
	}
	// Ciphertext bound to "admin" must not open for another user.
	if _, err := k2.Open(ct, []byte("mallory")); err == nil {
		t.Error("AAD mismatch should fail")
	}
	// A key file of the wrong size is rejected.
	bad := filepath.Join(t.TempDir(), "bad.key")
	os.WriteFile(bad, []byte("short"), 0o600)
	if _, err := LoadOrCreateKey(bad); err == nil {
		t.Error("short key accepted")
	}
}

func TestNewToken(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 || HashToken(tok) != hash || tok == hash {
		t.Errorf("token=%s hash=%s", tok, hash)
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewRateLimiter(6, 3) // 1 token per 10s, burst 3
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d blocked within burst", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("4th request allowed")
	}
	if !l.Allow("5.6.7.8") {
		t.Error("other IP blocked")
	}
	now = now.Add(10 * time.Second)
	if !l.Allow("1.2.3.4") {
		t.Error("not refilled after 10s")
	}
	if l.Allow("1.2.3.4") {
		t.Error("refilled too much")
	}
}
