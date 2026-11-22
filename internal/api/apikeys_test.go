package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/auth"
)

// keyReq makes a request with an API key and no cookies.
func keyReq(t *testing.T, env *testEnv, method, path, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, env.srv.URL+path, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		json.NewDecoder(resp.Body).Decode(&body)
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, body
}

func createKey(t *testing.T, c *client, scopes []string, days int) (string, int) {
	t.Helper()
	st, body := c.do("POST", "/api/v1/keys", map[string]any{"name": "siteherd", "scopes": scopes, "expires_in_days": days})
	if st != 201 {
		t.Fatalf("create key: %d %v", st, body)
	}
	return body["token"].(string), int(body["id"].(float64))
}

func TestAPIKeys(t *testing.T) {
	ag, _ := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	c := env.loggedIn(t)
	c.do("POST", "/api/v1/sites/scan", nil)

	token, id := createKey(t, c, []string{"sites:read", "ssl:read"}, 0)
	if !auth.LooksLikeAPIKey(token) {
		t.Fatalf("token format: %s", token)
	}

	// Only the hash is stored; the list never shows the token.
	var stored string
	env.st.DB.QueryRow(`SELECT token_hash FROM api_keys WHERE id=?`, id).Scan(&stored)
	if stored == token || stored != auth.HashToken(token) {
		t.Error("token not stored as a hash")
	}
	_, list := c.do("GET", "/api/v1/keys", nil)
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), token) || strings.Contains(string(b), stored) {
		t.Error("key list leaks the token or its hash")
	}

	// Allowed: reads within scope.
	if st, body := keyReq(t, env, "GET", "/api/v1/sites", token); st != 200 || len(body["sites"].([]any)) != 3 {
		t.Errorf("sites: %d %v", st, body)
	}
	if st, _ := keyReq(t, env, "GET", "/api/v1/ssl", token); st != 200 {
		t.Errorf("ssl: %d", st)
	}
	// Refused: other scopes, writes, session-only endpoints.
	cases := []struct {
		method, path, code string
		status             int
	}{
		{"GET", "/api/v1/databases", "insufficient_scope", 403},
		{"GET", "/api/v1/dashboard", "insufficient_scope", 403},
		{"POST", "/api/v1/sites", "session_required", 403},
		{"POST", "/api/v1/sites/scan", "session_required", 403},
		{"DELETE", "/api/v1/sites/1", "session_required", 403},
		{"GET", "/api/v1/keys", "session_required", 403},
		{"POST", "/api/v1/keys", "session_required", 403},
		{"GET", "/api/v1/sites/1/files", "session_required", 403},
		{"GET", "/api/v1/jobs/x/secret", "session_required", 403},
		{"GET", "/api/v1/auth/me", "session_required", 403},
	}
	for _, tc := range cases {
		st, body := keyReq(t, env, tc.method, tc.path, token)
		if st != tc.status || errCode(body) != tc.code {
			t.Errorf("%s %s: %d %v, want %d %s", tc.method, tc.path, st, body, tc.status, tc.code)
		}
	}

	// Bad tokens.
	for _, bad := range []string{"nope", "lyt_short", "lyt_" + strings.Repeat("A", 43)} {
		if st, body := keyReq(t, env, "GET", "/api/v1/sites", bad); st != 401 || errCode(body) != "invalid_api_key" {
			t.Errorf("bad token %q: %d %v", bad, st, body)
		}
	}

	// last_used is recorded; revoked keys stop working; both are audited.
	var used int64
	env.st.DB.QueryRow(`SELECT last_used_at FROM api_keys WHERE id=?`, id).Scan(&used)
	if used == 0 {
		t.Error("last_used_at not set")
	}
	if st, _ := c.do("DELETE", "/api/v1/keys/"+strconv.Itoa(id), nil); st != 204 {
		t.Errorf("revoke: %d", st)
	}
	if st, _ := keyReq(t, env, "GET", "/api/v1/sites", token); st != 401 {
		t.Errorf("revoked key still works: %d", st)
	}
	var audits int
	env.st.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('apikey.create','apikey.revoke')`).Scan(&audits)
	if audits != 2 {
		t.Errorf("audit rows = %d", audits)
	}
}

func TestAPIKeyExpiryAndValidation(t *testing.T) {
	ag, _ := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	c := env.loggedIn(t)

	for _, bad := range []map[string]any{
		{"name": "", "scopes": []string{"sites:read"}},
		{"name": "x", "scopes": []string{}},
		{"name": "x", "scopes": []string{"sites:write"}},
		{"name": "x", "scopes": []string{"sites:read"}, "expires_in_days": -1},
	} {
		if st, _ := c.do("POST", "/api/v1/keys", bad); st != 400 {
			t.Errorf("%v: %d", bad, st)
		}
	}

	token, _ := createKey(t, c, []string{"sites:read"}, 30)
	if st, _ := keyReq(t, env, "GET", "/api/v1/sites", token); st != 200 {
		t.Errorf("fresh key: %d", st)
	}
	env.now = env.now.Add(31 * 24 * time.Hour)
	if st, body := keyReq(t, env, "GET", "/api/v1/sites", token); st != 401 || errCode(body) != "api_key_expired" {
		t.Errorf("expired key: %d %v", st, body)
	}

}

// Every path in openapi.json must work with an API key, and the spec must
// list every key-readable route.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	ag, _ := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	c := env.loggedIn(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	var all []string
	for _, s := range apiScopes {
		all = append(all, s.Name)
	}
	token, _ := createKey(t, c, all, 0)

	resp, err := http.Get(env.srv.URL + "/api/v1/openapi.json")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("openapi: %v %v", resp, err)
	}
	var spec map[string]any
	json.NewDecoder(resp.Body).Decode(&spec)
	resp.Body.Close()
	paths := spec["paths"].(map[string]any)
	if len(paths) != 12 {
		t.Errorf("spec has %d paths, routes have 12 key-readable ones", len(paths))
	}
	for p := range paths {
		url := "/api/v1" + strings.ReplaceAll(p, "{id}", "1")
		if p == "/logs/stream" {
			continue // streams forever; covered by logs tests
		}
		if st, body := keyReq(t, env, "GET", url, token); st == 401 || st == 403 {
			t.Errorf("%s: %d %v", p, st, body)
		}
	}
}
