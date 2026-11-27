package api

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// The Account page: every logged-in user (admin or site login) manages
// their own profile, password, two-factor login and sessions here.
// Password and 2FA changes need the current password, and keep only the
// session that made them.

type sessionJSON struct {
	ID         string    `json:"id"` // a prefix of the token's hash, never the token
	Current    bool      `json:"current"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	Remember   bool      `json:"remember"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

const sessionIDLen = 12

// GET /api/v1/account
func (s *server) handleAccount(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	u := si.User
	list, err := s.Store.SessionsOfUser(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, "list sessions", err)
		return
	}
	sessions := make([]sessionJSON, 0, len(list))
	for _, ss := range list {
		sessions = append(sessions, sessionJSON{
			ID: ss.TokenHash[:sessionIDLen], Current: ss.TokenHash == si.Session.TokenHash, IP: ss.IP, UserAgent: ss.UserAgent,
			Remember: ss.Remember, CreatedAt: ss.CreatedAt, LastSeenAt: ss.LastSeenAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username": u.Username, "role": u.Role, "display_name": u.DisplayName, "email": u.Email,
		"totp_set_up": len(u.TOTPSecretEnc) > 0, "created_at": u.CreatedAt, "sessions": sessions,
	})
}

// PATCH /api/v1/account {display_name, email}
func (s *server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if utf8.RuneCountInString(name) > 64 || strings.ContainsFunc(name, unicode.IsControl) {
		writeError(w, http.StatusBadRequest, "invalid", "the display name must be at most 64 characters")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email != "" {
		if err := validate.Email(email); err != nil {
			writeError(w, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}
	si, _ := sessionFrom(r.Context())
	if err := s.Store.SetProfile(r.Context(), si.User.ID, store.Profile{DisplayName: name, Email: email}); err != nil {
		s.internalError(w, "update profile", err)
		return
	}
	s.auditPanel(r, "account.update", map[string]any{"display_name": name, "email": email})
	u := si.User
	u.DisplayName, u.Email = name, email
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

// checkCurrentPassword verifies the password of the logged-in user; wrong
// answers count towards the lockout, like failed logins.
func (s *server) checkCurrentPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	si, _ := sessionFrom(r.Context())
	u := si.User
	if s.Now().Before(u.LockedUntil) || len(password) > auth.MaxPasswordLen {
		auth.BurnTime(password)
		writeError(w, http.StatusBadRequest, "wrong_password", "the current password is wrong (or the account is temporarily locked)")
		return false
	}
	ok, err := auth.VerifyPassword(password, u.PasswordHash)
	if err != nil {
		s.internalError(w, "verify password", err)
		return false
	}
	if !ok {
		s.recordFailure(r, u)
		writeError(w, http.StatusBadRequest, "wrong_password", "the current password is wrong (or the account is temporarily locked)")
		return false
	}
	return true
}

// POST /api/v1/account/password {current_password, new_password}
func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) || !s.checkCurrentPassword(w, r, req.Current) {
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if req.New == req.Current {
		writeError(w, http.StatusBadRequest, "invalid", "the new password is the same as the current one")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		s.internalError(w, "hash password", err)
		return
	}
	si, _ := sessionFrom(r.Context())
	if err := s.Store.ChangePassword(r.Context(), si.User.ID, hash, si.Session.TokenHash); err != nil {
		s.internalError(w, "change password", err)
		return
	}
	s.auditPanel(r, "account.password", nil)
	s.Log.Info("password changed", "user", si.User.Username)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/account/2fa {current_password} — a new 2FA secret for a new
// app or phone. The old one keeps working until the new one is confirmed.
func (s *server) handleStart2FAChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
	}
	if !decodeJSON(w, r, &req) || !s.checkCurrentPassword(w, r, req.Current) {
		return
	}
	si, _ := sessionFrom(r.Context())
	u := si.User
	enr, err := auth.NewTOTP(u.Username)
	if err != nil {
		s.internalError(w, "new totp", err)
		return
	}
	enc, err := s.Keyring.Seal([]byte(enr.Secret), []byte(u.Username))
	if err != nil {
		s.internalError(w, "encrypt totp secret", err)
		return
	}
	if err := s.Store.SetSessionTOTPChange(r.Context(), si.Session.TokenHash, enc); err != nil {
		s.internalError(w, "save totp change", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"secret": enr.Secret, "uri": enr.URL})
}

// POST /api/v1/account/2fa/confirm {code} — a code from the new app
// replaces the old secret; other sessions are signed out.
func (s *server) handleConfirm2FAChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	si, _ := sessionFrom(r.Context())
	u := si.User
	if len(si.Session.TOTPSetupEnc) == 0 {
		writeError(w, http.StatusBadRequest, "totp_setup_missing", "start the change first")
		return
	}
	secret, err := s.Keyring.Open(si.Session.TOTPSetupEnc, []byte(u.Username))
	if err != nil {
		s.internalError(w, "decrypt totp change", err)
		return
	}
	step, ok := auth.CheckTOTP(string(secret), req.Code, s.Now(), 0)
	if !ok {
		s.recordFailure(r, u)
		writeError(w, http.StatusBadRequest, "invalid_code", "that code didn't match; check the device clock and try again")
		return
	}
	if err := s.Store.ReplaceTOTP(r.Context(), u.ID, si.Session.TOTPSetupEnc, step, si.Session.TokenHash); err != nil {
		s.internalError(w, "replace totp", err)
		return
	}
	s.auditPanel(r, "account.2fa", nil)
	s.Log.Info("2fa changed", "user", u.Username)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/account/sessions — sign out everywhere else.
func (s *server) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	n, err := s.Store.DeleteOtherSessions(r.Context(), si.User.ID, si.Session.TokenHash)
	if err != nil {
		s.internalError(w, "delete sessions", err)
		return
	}
	s.auditPanel(r, "account.sign_out_others", map[string]any{"sessions": n})
	writeJSON(w, http.StatusOK, map[string]any{"signed_out": n})
}

// DELETE /api/v1/account/sessions/{id} — sign out one other session.
func (s *server) handleSignOutSession(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	id := r.PathValue("id")
	list, err := s.Store.SessionsOfUser(r.Context(), si.User.ID)
	if err != nil {
		s.internalError(w, "list sessions", err)
		return
	}
	for _, ss := range list {
		if len(id) == sessionIDLen && strings.HasPrefix(ss.TokenHash, id) && ss.TokenHash != si.Session.TokenHash {
			if err := s.Store.DeleteSession(r.Context(), ss.TokenHash); err != nil {
				s.internalError(w, "delete session", err)
				return
			}
			s.auditPanel(r, "account.sign_out_session", map[string]any{"ip": ss.IP})
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "not_found", "no such session (use Log out for this one)")
}
