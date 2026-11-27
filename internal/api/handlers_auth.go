package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
)

func hashToken(t string) string { return auth.HashToken(t) }

type userJSON struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"` // admin | site
	// Site accounts: the main site, and how many subdomains they may add.
	SiteID        int64 `json:"site_id,omitempty"`
	MaxSubdomains int   `json:"max_subdomains,omitempty"`
}

func toUserJSON(u store.User) userJSON {
	j := userJSON{ID: u.ID, Username: u.Username, Role: u.Role}
	if !u.IsAdmin() {
		j.SiteID, j.MaxSubdomains = u.SiteID, u.MaxSubdomains
	}
	return j
}

// One message for every login failure, so responses don't reveal whether a
// username exists or an account is locked.
const invalidCreds = "invalid username or password, or account temporarily locked"

// POST /api/v1/auth/login {username, password}
// Step 1 of 2: on success, a short-lived "pending" session is created and the
// client must then submit a TOTP code.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	now := s.Now()

	if len(req.Password) > auth.MaxPasswordLen {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	u, err := s.Store.UserByUsername(ctx, req.Username)
	if errors.Is(err, store.ErrNotFound) {
		auth.BurnTime(req.Password)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	if err != nil {
		s.internalError(w, "load user", err)
		return
	}
	if now.Before(u.LockedUntil) {
		auth.BurnTime(req.Password)
		s.Log.Warn("login attempt on locked account", "user", u.Username, "ip", clientIP(r, s.Config.TrustedProxies))
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	ok, err := auth.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil {
		s.internalError(w, "verify password", err)
		return
	}
	if !ok {
		s.recordFailure(r, u)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	if len(u.TOTPSecretEnc) == 0 {
		writeError(w, http.StatusForbidden, "totp_not_enrolled", "two-factor authentication is not set up; run `layte admin reset-2fa`")
		return
	}
	// The failure counter is NOT reset here; only after the TOTP step succeeds.
	// Otherwise someone with the password could guess TOTP codes forever.

	if si, ok := sessionFrom(ctx); ok { // replace any existing session
		s.Store.DeleteSession(ctx, si.Session.TokenHash)
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		s.internalError(w, "new token", err)
		return
	}
	csrf, _, err := auth.NewToken()
	if err != nil {
		s.internalError(w, "new csrf token", err)
		return
	}
	if err := s.Store.CreateSession(ctx, store.Session{
		TokenHash: hash, UserID: u.ID, CSRFToken: csrf, TOTPPending: true,
		CreatedAt: now, LastSeenAt: now,
		IP: clientIP(r, s.Config.TrustedProxies), UserAgent: truncate(r.UserAgent(), 255),
	}); err != nil {
		s.internalError(w, "create session", err)
		return
	}
	s.setSessionCookie(w, token, 0) // pending 2FA: a browser-session cookie
	writeJSON(w, http.StatusOK, map[string]any{"totp_required": true, "csrf_token": csrf})
}

// POST /api/v1/auth/totp {code, remember}
// Step 2 of 2: verifies the code and upgrades the session (with a new token).
// remember = "remember this browser for 30 days" (see rememberMax).
func (s *server) handleTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Remember bool   `json:"remember"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	now := s.Now()
	si, _ := sessionFrom(ctx)
	u := si.User

	if now.Before(u.LockedUntil) {
		s.Store.DeleteSession(ctx, si.Session.TokenHash)
		s.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	secret, err := s.Keyring.Open(u.TOTPSecretEnc, []byte(u.Username))
	if err != nil {
		s.internalError(w, "decrypt totp secret", err)
		return
	}
	step, ok := auth.CheckTOTP(string(secret), req.Code, now, u.TOTPLastStep)
	if ok {
		// Atomic claim: if two requests race with the same code, only one wins.
		ok, err = s.Store.ClaimTOTPStep(ctx, u.ID, step)
		if err != nil {
			s.internalError(w, "claim totp step", err)
			return
		}
	}
	if !ok {
		if locked := s.recordFailure(r, u); locked {
			s.Store.DeleteSession(ctx, si.Session.TokenHash)
			s.clearSessionCookie(w)
		}
		writeError(w, http.StatusUnauthorized, "invalid_code", "invalid or already used code")
		return
	}

	if err := s.Store.ResetLoginFailures(ctx, u.ID); err != nil {
		s.internalError(w, "reset failures", err)
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		s.internalError(w, "new token", err)
		return
	}
	csrf, _, err := auth.NewToken()
	if err != nil {
		s.internalError(w, "new csrf token", err)
		return
	}
	if err := s.Store.PromoteSession(ctx, si.Session.TokenHash, hash, csrf, now, req.Remember); err != nil {
		s.internalError(w, "promote session", err)
		return
	}
	var cookieAge time.Duration // 0 = browser-session cookie
	if req.Remember {
		cookieAge = rememberMax // survives closing the browser
	}
	s.setSessionCookie(w, token, cookieAge)
	s.Log.Info("login", "user", u.Username, "ip", clientIP(r, s.Config.TrustedProxies), "remember", req.Remember)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u), "csrf_token": csrf})
}

// POST /api/v1/auth/logout
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	if err := s.Store.DeleteSession(r.Context(), si.Session.TokenHash); err != nil {
		s.internalError(w, "delete session", err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/auth/me — the current user plus the CSRF token for later requests.
func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(si.User), "csrf_token": si.Session.CSRFToken})
}

// recordFailure counts a failed password or TOTP attempt and reports whether
// the account just got locked.
func (s *server) recordFailure(r *http.Request, u store.User) bool {
	locked, err := s.Store.RecordLoginFailure(r.Context(), u.ID, maxFailures, lockFor)
	if err != nil {
		s.Log.Error("record login failure", "err", err)
		return false
	}
	lvl := "login failed"
	if locked {
		lvl = "account locked after repeated failures"
	}
	s.Log.Warn(lvl, "user", u.Username, "ip", clientIP(r, s.Config.TrustedProxies))
	return locked
}

func (s *server) internalError(w http.ResponseWriter, what string, err error) {
	s.Log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
