package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"layte-panel/internal/auth"
	"layte-panel/internal/config"
	"layte-panel/internal/store"
)

const testPassword = "correct horse battery staple"

type testEnv struct {
	srv    *httptest.Server
	st     *store.Store
	secret string
	now    time.Time
}

// newTestEnv starts the API with a fresh DB and one admin ("admin").
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	kr, err := auth.LoadOrCreateKey(filepath.Join(t.TempDir(), "k"))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword(testPassword)
	enr, _ := auth.NewTOTP("admin")
	enc, _ := kr.Seal([]byte(enr.Secret), []byte("admin"))
	if _, err := st.CreateUser(ctx, "admin", hash, enc); err != nil {
		t.Fatal(err)
	}

	env := &testEnv{st: st, secret: enr.Secret, now: time.Now()}
	cfg := config.DevDefaults()
	h := New(Deps{
		Config:  cfg,
		Store:   st,
		Keyring: kr,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
		Now:     func() time.Time { return env.now },
	})
	env.srv = httptest.NewServer(h)
	t.Cleanup(env.srv.Close)
	return env
}

// client is a browser-like client: it keeps cookies and the CSRF token.
type client struct {
	t    *testing.T
	env  *testEnv
	http *http.Client
	csrf string
}

func (e *testEnv) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, env: e, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.env.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set(csrfHeader, c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if tok, ok := out["csrf_token"].(string); ok {
		c.csrf = tok
	}
	return resp.StatusCode, out
}

func (c *client) code(offset time.Duration) string {
	code, _ := auth.TOTPCode(c.env.secret, c.env.now.Add(offset))
	return code
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func TestLoginFlow(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)

	if st, body := c.do("GET", "/api/v1/auth/me", nil); st != 401 || errCode(body) != "unauthenticated" {
		t.Fatalf("me before login: %d %v", st, body)
	}
	st, body := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if st != 200 || body["totp_required"] != true {
		t.Fatalf("login: %d %v", st, body)
	}
	// Password alone is not enough.
	if st, body := c.do("GET", "/api/v1/auth/me", nil); st != 401 || errCode(body) != "totp_required" {
		t.Fatalf("me after password: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)}); st != 200 {
		t.Fatalf("totp: %d %v", st, body)
	}
	st, body = c.do("GET", "/api/v1/auth/me", nil)
	if st != 200 || body["user"].(map[string]any)["username"] != "admin" {
		t.Fatalf("me: %d %v", st, body)
	}
	if st, _ := c.do("POST", "/api/v1/auth/logout", nil); st != 204 {
		t.Fatalf("logout: %d", st)
	}
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Fatalf("me after logout: %d", st)
	}
}

func TestSessionTokenRotatesAfterTOTP(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	before := c.http.Jar.Cookies(mustURL(env.srv.URL))[0].Value
	c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)})
	after := c.http.Jar.Cookies(mustURL(env.srv.URL))[0].Value
	if before == after {
		t.Error("session token must change after the second factor (session fixation)")
	}
}

func TestBadCredentials(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	for _, creds := range []map[string]string{
		{"username": "admin", "password": "wrong password!!"},
		{"username": "nobody", "password": testPassword},
	} {
		st, body := c.do("POST", "/api/v1/auth/login", creds)
		if st != 401 || errCode(body) != "invalid_credentials" {
			t.Errorf("%v: %d %v", creds, st, body)
		}
	}
}

func TestTOTPReplayRejected(t *testing.T) {
	env := newTestEnv(t)
	code := env.client(t).code(0)

	c1 := env.client(t)
	c1.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if st, _ := c1.do("POST", "/api/v1/auth/totp", map[string]string{"code": code}); st != 200 {
		t.Fatalf("first use: %d", st)
	}
	c2 := env.client(t)
	c2.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if st, body := c2.do("POST", "/api/v1/auth/totp", map[string]string{"code": code}); st != 401 || errCode(body) != "invalid_code" {
		t.Fatalf("replay: %d %v", st, body)
	}
}

func TestLockoutAfterRepeatedTOTPFailures(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	for i := 0; i < maxFailures; i++ {
		c.do("POST", "/api/v1/auth/totp", map[string]string{"code": "000000"})
	}
	// Locked: even the right password + code fails now.
	st, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if st != 401 {
		t.Fatalf("login while locked: %d", st)
	}
	// After the lock expires it works again.
	env.now = env.now.Add(lockFor + time.Minute)
	if st, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}); st != 200 {
		t.Fatalf("login after lock expiry: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)}); st != 200 {
		t.Fatalf("totp after lock expiry: %d", st)
	}
}

func TestCSRF(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)})

	good := c.csrf
	c.csrf = ""
	if st, body := c.do("POST", "/api/v1/auth/logout", nil); st != 403 || errCode(body) != "csrf" {
		t.Errorf("no token: %d %v", st, body)
	}
	c.csrf = "nope"
	if st, _ := c.do("POST", "/api/v1/auth/logout", nil); st != 403 {
		t.Errorf("wrong token: %d", st)
	}

	// Cross-origin POST is rejected even with the right token.
	req, _ := http.NewRequest("POST", env.srv.URL+"/api/v1/auth/logout", nil)
	req.Header.Set(csrfHeader, good)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("cross-origin: %d", resp.StatusCode)
	}

	c.csrf = good
	if st, _ := c.do("POST", "/api/v1/auth/logout", nil); st != 204 {
		t.Errorf("good token: %d", st)
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)})
	env.now = env.now.Add(31 * time.Minute) // idle timeout is 30m
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Errorf("expired session still valid: %d", st)
	}
}

func TestPendingSessionExpires(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	env.now = env.now.Add(pendingTTL + time.Second)
	if st, _ := c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)}); st != 401 {
		t.Errorf("TOTP accepted on expired pending session: %d", st)
	}
}

func TestRateLimit(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	limited := false
	for i := 0; i < 15; i++ {
		if st, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "nobody", "password": "x"}); st == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("login was never rate limited")
	}
}

func TestClientIP(t *testing.T) {
	trusted := []string{"127.0.0.1"}
	tests := []struct {
		remote, xff, want string
	}{
		{"203.0.113.9:5555", "1.1.1.1", "203.0.113.9"},              // untrusted peer: XFF ignored
		{"127.0.0.1:5555", "198.51.100.7", "198.51.100.7"},          // via our nginx
		{"127.0.0.1:5555", "6.6.6.6, 198.51.100.7", "198.51.100.7"}, // spoofed left entry ignored
		{"127.0.0.1:5555", "", "127.0.0.1"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		if tt.xff != "" {
			r.Header.Set("X-Forwarded-For", tt.xff)
		}
		if got := clientIP(r, trusted); got != tt.want {
			t.Errorf("clientIP(%s, %q) = %s, want %s", tt.remote, tt.xff, got, tt.want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Get(env.srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}
