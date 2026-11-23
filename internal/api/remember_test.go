package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// totpRaw sends the 2FA step by hand, so the Set-Cookie header can be read.
func totpRaw(t *testing.T, c *client, remember bool) *http.Cookie {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"code": c.code(0), "remember": remember})
	req, _ := http.NewRequest("POST", c.env.srv.URL+"/api/v1/auth/totp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, c.csrf)
	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("totp: %v %v", resp.Status, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	c.csrf, _ = out["csrf_token"].(string)
	for _, ck := range resp.Cookies() {
		if ck.Name == cookieName {
			return ck
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestRememberBrowser(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	ck := totpRaw(t, c, true)
	if ck.MaxAge != int(rememberMax.Seconds()) || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
		t.Errorf("remembered cookie: MaxAge=%d HttpOnly=%v SameSite=%v", ck.MaxAge, ck.HttpOnly, ck.SameSite)
	}

	// Two days unused: a normal session (30 min idle) would be gone.
	env.now = env.now.Add(48 * time.Hour)
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 200 {
		t.Fatalf("after 2 idle days: %d", st)
	}
	// Used every 6 days, it still ends after 30 days in total.
	for i := 0; i < 4; i++ {
		env.now = env.now.Add(6 * 24 * time.Hour)
		if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 200 {
			t.Fatalf("day %d: %d", 2+6*(i+1), st)
		}
	}
	env.now = env.now.Add(5 * 24 * time.Hour) // day 31
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Errorf("after 30 days: %d, want 401", st)
	}

	// 8 days unused also ends it.
	c2 := env.client(t)
	env.now = env.now.Add(time.Minute) // a fresh TOTP step
	c2.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	totpRaw(t, c2, true)
	env.now = env.now.Add(8 * 24 * time.Hour)
	if st, _ := c2.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Errorf("after 8 idle days: %d, want 401", st)
	}
}

// Without "remember", the cookie is a browser-session cookie and the usual
// 30-minute idle limit applies.
func TestNoRememberIsSessionCookie(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if ck := totpRaw(t, c, false); ck.MaxAge != 0 || !ck.Expires.IsZero() {
		t.Errorf("cookie should end with the browser: MaxAge=%d Expires=%v", ck.MaxAge, ck.Expires)
	}
	env.now = env.now.Add(31 * time.Minute)
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Errorf("after 31 idle minutes: %d, want 401", st)
	}
}
