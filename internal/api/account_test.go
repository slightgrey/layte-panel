package api

import (
	"testing"
	"time"
)

// nextLogin logs in again a TOTP step later (a code can't be used twice).
func (e *testEnv) nextLogin(t *testing.T) *client {
	t.Helper()
	e.now = e.now.Add(30 * time.Second)
	return e.loggedIn(t)
}

func TestAccountPage(t *testing.T) {
	e := newTestEnv(t)
	c := e.loggedIn(t)
	other := e.nextLogin(t) // a second browser

	st, body := c.do("GET", "/api/v1/account", nil)
	if st != 200 || body["username"] != "admin" || body["totp_set_up"] != true || len(body["sessions"].([]any)) != 2 {
		t.Fatalf("account: %d %v", st, body)
	}

	// Profile.
	if st, _ := c.do("PATCH", "/api/v1/account", map[string]string{"display_name": "Vince", "email": "nope"}); st != 400 {
		t.Errorf("bad email: %d", st)
	}
	if st, body := c.do("PATCH", "/api/v1/account", map[string]string{"display_name": " Vince ", "email": "v@example.com"}); st != 200 || body["user"].(map[string]any)["display_name"] != "Vince" {
		t.Errorf("profile: %d %v", st, body)
	}
	if _, me := c.do("GET", "/api/v1/auth/me", nil); me["user"].(map[string]any)["display_name"] != "Vince" {
		t.Errorf("me: %v", me)
	}

	// (Password and 2FA calls are rate limited per IP, so few of them here.)
	// Password: needs the current one; keeps this session, ends the other.
	if st, body := c.do("POST", "/api/v1/account/password", map[string]string{"current_password": "wrong", "new_password": "a brand new passphrase"}); st != 400 || errCode(body) != "wrong_password" {
		t.Errorf("wrong current password: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/account/password", map[string]string{"current_password": testPassword, "new_password": "a brand new passphrase"}); st != 204 {
		t.Fatalf("change password: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/account", nil); st != 200 {
		t.Errorf("own session ended by the password change: %d", st)
	}
	if st, _ := other.do("GET", "/api/v1/account", nil); st != 401 {
		t.Errorf("other session survived the password change: %d", st)
	}

	// 2FA: a new secret, confirmed with a code from it.
	st, body = c.do("POST", "/api/v1/account/2fa", map[string]string{"current_password": "a brand new passphrase"})
	if st != 200 || body["secret"] == "" {
		t.Fatalf("2fa start: %d %v", st, body)
	}
	newSecret := body["secret"].(string)
	if st, _ := c.do("POST", "/api/v1/account/2fa/confirm", map[string]string{"code": c.code(0)}); st != 400 {
		t.Errorf("confirmed with the old app's code: %d", st)
	}
	c.secret = newSecret
	if st, body := c.do("POST", "/api/v1/account/2fa/confirm", map[string]string{"code": c.code(0)}); st != 204 {
		t.Fatalf("2fa confirm: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/account", nil); st != 200 {
		t.Errorf("own session ended by the 2FA change: %d", st)
	}
}

func TestAccountSessions(t *testing.T) {
	e := newTestEnv(t)
	c := e.loggedIn(t)
	b := e.nextLogin(t)
	d := e.nextLogin(t)

	_, body := c.do("GET", "/api/v1/account", nil)
	var otherID string
	for _, x := range body["sessions"].([]any) {
		if s := x.(map[string]any); s["current"] != true {
			otherID = s["id"].(string)
			break
		}
	}
	if st, _ := c.do("DELETE", "/api/v1/account/sessions/"+otherID, nil); st != 204 {
		t.Fatalf("sign out one: %d", st)
	}
	alive := 0
	for _, x := range []*client{b, d} {
		if st, _ := x.do("GET", "/api/v1/account", nil); st == 200 {
			alive++
		}
	}
	if alive != 1 {
		t.Errorf("%d other sessions alive, want 1", alive)
	}
	if st, body := c.do("DELETE", "/api/v1/account/sessions", nil); st != 200 || body["signed_out"] != float64(1) {
		t.Errorf("sign out others: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/account", nil); st != 200 {
		t.Errorf("own session gone: %d", st)
	}
}
