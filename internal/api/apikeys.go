package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
)

// Scopes an API key can have. All are read-only: v1 keys can't change anything.
type apiScope struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var apiScopes = []apiScope{
	{"system:read", "Dashboard: server info, services, versions, pending updates"},
	{"sites:read", "Sites, their details and nginx configs; installed PHP versions"},
	{"ssl:read", "Certificates and their expiry"},
	{"databases:read", "Databases and their sizes"},
	{"cron:read", "Cron jobs"},
	{"security:read", "Firewall rules, failed SSH logins, security updates"},
	{"logs:read", "Log files and their contents (may contain IPs and URLs)"},
}

func validScope(s string) bool {
	return slices.ContainsFunc(apiScopes, func(x apiScope) bool { return x.Name == s })
}

type apiKeyCtx struct{}

// keyFrom returns the API key of the request, if it used one.
func keyFrom(ctx context.Context) (store.APIKey, bool) {
	k, ok := ctx.Value(apiKeyCtx{}).(store.APIKey)
	return k, ok
}

// loadAPIKey authenticates "Authorization: Bearer lyt_…" requests. A bad,
// expired or revoked key is rejected right away (never treated as anonymous).
func (s *server) loadAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || !auth.LooksLikeAPIKey(token) {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "use Authorization: Bearer lyt_…")
			return
		}
		k, err := s.Store.APIKeyByHash(r.Context(), auth.HashToken(token))
		now := s.Now()
		switch {
		case errors.Is(err, store.ErrNotFound), err == nil && k.RevokedAt != nil:
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "unknown or revoked API key")
			return
		case err != nil:
			s.internalError(w, "load api key", err)
			return
		case k.ExpiresAt != nil && now.After(*k.ExpiresAt):
			writeError(w, http.StatusUnauthorized, "api_key_expired", "this API key has expired")
			return
		}
		if !s.keyLimiter.Allow("key:" + strconv.FormatInt(k.ID, 10)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "API key rate limit: 120 requests per minute")
			return
		}
		if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
			s.Store.TouchAPIKey(r.Context(), k.ID, now)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyCtx{}, k)))
	})
}

// scoped allows a logged-in admin, or an API key with the given scope (GET only).
func (s *server) scoped(scope string, h http.HandlerFunc) http.Handler {
	if !validScope(scope) {
		panic("unknown scope " + scope) // programmer error at startup
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if k, ok := keyFrom(r.Context()); ok {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeError(w, http.StatusForbidden, "read_only_key", "API keys are read-only")
				return
			}
			if !slices.Contains(k.Scopes, scope) {
				writeError(w, http.StatusForbidden, "insufficient_scope", "this API key needs the "+scope+" scope")
				return
			}
			h(w, r)
			return
		}
		s.requireAuth(h).ServeHTTP(w, r)
	})
}

// GET /api/v1/keys
func (s *server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.ListAPIKeys(r.Context())
	if err != nil {
		s.internalError(w, "list api keys", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "scopes": apiScopes})
}

// POST /api/v1/keys {name, scopes, expires_in_days} — returns the token once.
func (s *server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expires_in_days"` // 0 = never
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 64 || strings.ContainsAny(req.Name, "\n\r\t") {
		writeError(w, http.StatusBadRequest, "invalid", "name must be 1-64 characters")
		return
	}
	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "invalid", "choose at least one scope")
		return
	}
	scopes := []string{}
	for _, sc := range req.Scopes {
		if !validScope(sc) {
			writeError(w, http.StatusBadRequest, "invalid", "unknown scope "+sc)
			return
		}
		if !slices.Contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		writeError(w, http.StatusBadRequest, "invalid", "expiry must be 0 (never) to 3650 days")
		return
	}
	token, hash, prefix, err := auth.NewAPIKey()
	if err != nil {
		s.internalError(w, "new api key", err)
		return
	}
	si, _ := sessionFrom(r.Context())
	k := store.APIKey{Name: req.Name, Prefix: prefix, TokenHash: hash, Scopes: scopes, CreatedBy: si.User.ID}
	if req.ExpiresInDays > 0 {
		t := s.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		k.ExpiresAt = &t
	}
	id, err := s.Store.CreateAPIKey(r.Context(), k)
	if err != nil {
		s.internalError(w, "create api key", err)
		return
	}
	s.auditPanel(r, "apikey.create", map[string]any{"id": id, "name": req.Name, "scopes": scopes, "expires_in_days": req.ExpiresInDays})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "token": token, "prefix": prefix})
}

// DELETE /api/v1/keys/{id} — revoke (kept in the list, marked revoked).
func (s *server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	k, err := s.Store.APIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such API key")
		return
	}
	if err != nil {
		s.internalError(w, "load api key", err)
		return
	}
	if err := s.Store.RevokeAPIKey(r.Context(), id); err != nil {
		s.internalError(w, "revoke api key", err)
		return
	}
	s.auditPanel(r, "apikey.revoke", map[string]any{"id": id, "name": k.Name})
	w.WriteHeader(http.StatusNoContent)
}

// auditPanel records panel-level changes (not agent actions) in the audit log.
func (s *server) auditPanel(r *http.Request, action string, args map[string]any) {
	si, _ := sessionFrom(r.Context())
	if err := s.Store.InsertAudit(r.Context(), store.AuditRow{
		Time: s.Now(), UserID: si.User.ID, Username: si.User.Username, Action: action, ArgsJSON: store.ToJSON(args), OK: true,
	}); err != nil {
		s.Log.Error("audit", "action", action, "err", err)
	}
}
