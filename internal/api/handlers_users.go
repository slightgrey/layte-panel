package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
)

// Site accounts: logins limited to one site and its subdomains. Admins manage
// them here; admins themselves are managed with `layte admin` (CLI).

// MaxSubdomainLimit caps the per-account subdomain limit an admin can set.
const MaxSubdomainLimit = 100

// DefaultSubdomainLimit is the limit of a new site account.
const DefaultSubdomainLimit = 5

// DefaultLimits are a new site login's resource limits.
var DefaultLimits = store.Limits{CPUPercent: 100, MemoryMB: 512}

// limitsRequest is the optional limits of a create or update request.
type limitsRequest struct {
	CPUPercent *int `json:"cpu_percent"`
	MemoryMB   *int `json:"memory_mb"`
	DiskMB     *int `json:"disk_mb"`
}

// apply returns l with the request's fields set, or an error message.
func (r limitsRequest) apply(l store.Limits) (store.Limits, string) {
	if r.CPUPercent != nil {
		l.CPUPercent = *r.CPUPercent
	}
	if r.MemoryMB != nil {
		l.MemoryMB = *r.MemoryMB
	}
	if r.DiskMB != nil {
		l.DiskMB = *r.DiskMB
	}
	switch {
	case l.CPUPercent < 0 || l.CPUPercent > 800:
		return l, "the CPU limit must be 0-800% (100% = one CPU; 0 = none)"
	case l.MemoryMB != 0 && (l.MemoryMB < 64 || l.MemoryMB > 65536):
		return l, "the memory limit must be 64-65536 MB (0 = none)"
	case l.DiskMB < 0 || l.DiskMB > 16<<20:
		return l, "the disk limit must be 0-16777216 MB (0 = none)"
	}
	return l, ""
}

type accountJSON struct {
	ID            int64         `json:"id"`
	Username      string        `json:"username"`
	Role          string        `json:"role"`
	SiteID        int64         `json:"site_id,omitempty"`
	SiteDomain    string        `json:"site_domain,omitempty"`
	MaxSubdomains int           `json:"max_subdomains"`
	Subdomains    int           `json:"subdomains"`
	Limits        *store.Limits `json:"limits,omitempty"`
	TOTPSetUp     bool          `json:"totp_set_up"`
	Locked        bool          `json:"locked"`
	// SetupExpires: a setup link is pending until then.
	SetupExpires *time.Time `json:"setup_expires,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *server) toAccountJSON(r *http.Request, u store.User) accountJSON {
	j := accountJSON{
		ID: u.ID, Username: u.Username, Role: u.Role, SiteID: u.SiteID,
		TOTPSetUp: len(u.TOTPSecretEnc) > 0, Locked: s.Now().Before(u.LockedUntil), CreatedAt: u.CreatedAt,
	}
	if !u.SetupExpires.IsZero() {
		j.SetupExpires = &u.SetupExpires
	}
	if !u.IsAdmin() {
		j.Limits = &u.Limits
		j.MaxSubdomains = u.MaxSubdomains
		if site, err := s.Store.SiteByID(r.Context(), u.SiteID); err == nil {
			j.SiteDomain = site.Domain
		}
		j.Subdomains, _ = s.Store.CountSubdomains(r.Context(), u.SiteID)
	}
	return j
}

// setupLinkTTL is how long a setup link works.
const setupLinkTTL = 72 * time.Hour

// issueSetupLink gives the user a new one-time setup link (the old one stops
// working) and returns its URL. Only the token's hash is stored.
func (s *server) issueSetupLink(r *http.Request, u store.User) (string, time.Time, error) {
	token, hash, err := auth.NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.Now().Add(setupLinkTTL)
	if err := s.Store.SetSetupToken(r.Context(), u.ID, hash, expires); err != nil {
		return "", time.Time{}, err
	}
	scheme := "https"
	if s.Config.Dev {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/setup/" + token, expires, nil
}

// GET /api/v1/users
func (s *server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		s.internalError(w, "list users", err)
		return
	}
	out := make([]accountJSON, 0, len(users))
	for _, u := range users {
		out = append(out, s.toAccountJSON(r, u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// POST /api/v1/users {username, site_id, max_subdomains?} — creates a site
// account; 201 with its setup link (shown once), where the user chooses a
// password and sets up 2FA. Nobody else ever knows the password.
func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		SiteID        int64  `json:"site_id"`
		MaxSubdomains *int   `json:"max_subdomains"`
		limitsRequest
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	limits, bad := req.limitsRequest.apply(DefaultLimits)
	if bad != "" {
		writeError(w, http.StatusBadRequest, "invalid", bad)
		return
	}
	if err := auth.ValidateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	limit := DefaultSubdomainLimit
	if req.MaxSubdomains != nil {
		limit = *req.MaxSubdomains
	}
	if limit < 0 || limit > MaxSubdomainLimit {
		writeError(w, http.StatusBadRequest, "invalid", "the subdomain limit must be 0-"+strconv.Itoa(MaxSubdomainLimit))
		return
	}
	site, err := s.Store.SiteByID(r.Context(), req.SiteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "invalid", "no such site")
		return
	}
	if err != nil {
		s.internalError(w, "load site", err)
		return
	}
	switch {
	case site.ParentSiteID != 0:
		writeError(w, http.StatusBadRequest, "invalid", site.Domain+" is a subdomain site; create the login for its main site")
		return
	case !site.Managed && !site.Adopted:
		writeError(w, http.StatusConflict, "site_unmanaged", site.Domain+" was imported and is read-only; adopt it first")
		return
	case site.SystemUser == "":
		writeError(w, http.StatusConflict, "site_unmanaged", site.Domain+" has no Linux user")
		return
	}
	if _, err := s.Store.UserBySite(r.Context(), site.ID); err == nil {
		writeError(w, http.StatusConflict, "account_exists", site.Domain+" already has a login")
		return
	}
	hash, err := auth.HashPassword(rand.Text()) // unusable until the setup link sets one
	if err != nil {
		s.internalError(w, "hash password", err)
		return
	}
	id, err := s.Store.CreateSiteUser(r.Context(), req.Username, hash, site.ID, limit)
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, "username_taken", "username "+req.Username+" is taken")
		return
	}
	if err != nil {
		s.internalError(w, "create user", err)
		return
	}
	s.auditPanel(r, "user.create", map[string]any{"id": id, "username": req.Username, "site": site.Domain, "max_subdomains": limit})
	// The login's sites share a Linux user: no PHP shell functions, which
	// could reach past each site's own folder.
	if err := s.sites.BlockAccountShell(r.Context(), actorOf(r), site.ID); err != nil {
		s.Log.Warn("block php shell functions", "site", site.Domain, "err", err)
	}
	u, _ := s.Store.UserByID(r.Context(), id)
	// Its PHP and cron jobs move into the login's own limits.
	if err := s.sites.SetAccountLimits(r.Context(), actorOf(r), u, limits); err != nil {
		s.Log.Warn("apply limits", "user", u.Username, "err", err)
	}
	u, _ = s.Store.UserByID(r.Context(), id)
	link, expires, err := s.issueSetupLink(r, u)
	if err != nil {
		s.internalError(w, "setup link", err)
		return
	}
	u.SetupExpires = expires
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"user": s.toAccountJSON(r, u), "setup_url": link, "expires_at": expires})
}

// siteAccount loads the {id} user; only site accounts can be changed here.
func (s *server) siteAccount(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u, err := s.Store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return u, false
	}
	if err != nil {
		s.internalError(w, "load user", err)
		return u, false
	}
	if u.IsAdmin() {
		writeError(w, http.StatusConflict, "admin_account", "admins are managed with `layte admin` on the server")
		return u, false
	}
	return u, true
}

// PATCH /api/v1/users/{id} {max_subdomains}
func (s *server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		MaxSubdomains *int `json:"max_subdomains"`
		limitsRequest
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.MaxSubdomains != nil {
		if *req.MaxSubdomains < 0 || *req.MaxSubdomains > MaxSubdomainLimit {
			writeError(w, http.StatusBadRequest, "invalid", "the subdomain limit must be 0-"+strconv.Itoa(MaxSubdomainLimit))
			return
		}
		if err := s.Store.SetMaxSubdomains(r.Context(), u.ID, *req.MaxSubdomains); err != nil {
			s.internalError(w, "update user", err)
			return
		}
		u.MaxSubdomains = *req.MaxSubdomains
	}
	if req.CPUPercent != nil || req.MemoryMB != nil || req.DiskMB != nil {
		limits, bad := req.limitsRequest.apply(u.Limits)
		if bad != "" {
			writeError(w, http.StatusBadRequest, "invalid", bad)
			return
		}
		if err := s.sites.SetAccountLimits(r.Context(), actorOf(r), u, limits); err != nil {
			s.writeSiteError(w, "apply limits", err)
			return
		}
		u.Limits = limits
	}
	s.auditPanel(r, "user.update", map[string]any{"id": u.ID, "username": u.Username, "max_subdomains": u.MaxSubdomains, "limits": u.Limits})
	writeJSON(w, http.StatusOK, s.toAccountJSON(r, u))
}

// POST /api/v1/users/{id}/password — a password reset link (shown once).
// The current password keeps working until the link is used; then every
// session is logged out.
func (s *server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	link, expires, err := s.issueSetupLink(r, u)
	if err != nil {
		s.internalError(w, "setup link", err)
		return
	}
	s.auditPanel(r, "user.password_reset_link", map[string]any{"id": u.ID, "username": u.Username})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"setup_url": link, "expires_at": expires})
}

// setupUser loads the user of the {token} setup link (404 when unknown,
// used or expired).
func (s *server) setupUser(w http.ResponseWriter, r *http.Request) (store.User, string, bool) {
	hash := auth.HashToken(r.PathValue("token"))
	u, err := s.Store.UserBySetupToken(r.Context(), hash, s.Now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_invalid", "this link was already used, has expired or was replaced; ask your admin for a new one")
		return u, "", false
	}
	if err != nil {
		s.internalError(w, "setup link", err)
		return u, "", false
	}
	return u, hash, true
}

// GET /api/v1/setup/{token} — who the link is for, and until when.
func (s *server) handleSetupLink(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.setupUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "expires_at": u.SetupExpires, "totp_set_up": len(u.TOTPSecretEnc) > 0})
}

// POST /api/v1/setup/{token} {password} — set the password; the link is used
// up and the 2FA step follows (set up first for a new login).
func (s *server) handleUseSetupLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u, tokenHash, ok := s.setupUser(w, r)
	if !ok {
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internalError(w, "hash password", err)
		return
	}
	used, err := s.Store.UseSetupToken(r.Context(), u.ID, tokenHash, hash)
	if err != nil {
		s.internalError(w, "use setup link", err)
		return
	}
	if !used { // used at the same moment from somewhere else
		writeError(w, http.StatusNotFound, "link_invalid", "this link was already used; ask your admin for a new one")
		return
	}
	s.Log.Info("password set via setup link", "user", u.Username, "ip", clientIP(r, s.Config.TrustedProxies))
	s.startPending(w, r, u)
}

// POST /api/v1/users/{id}/reset-2fa — the user sets 2FA up again at the next
// login (e.g. a lost phone); logs them out everywhere.
func (s *server) handleResetUser2FA(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	if err := s.Store.ClearTOTP(r.Context(), u.ID); err != nil {
		s.internalError(w, "reset 2fa", err)
		return
	}
	s.auditPanel(r, "user.2fa_reset", map[string]any{"id": u.ID, "username": u.Username})
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/users/{id} — removes the login; the sites stay.
func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	// Its sites' PHP goes back to the shared PHP-FPM services, without limits.
	if err := s.sites.ReleaseAccount(r.Context(), actorOf(r), u); err != nil {
		s.writeSiteError(w, "release limits", err)
		return
	}
	if err := s.Store.DeleteUser(r.Context(), u.ID); err != nil {
		s.internalError(w, "delete user", err)
		return
	}
	s.auditPanel(r, "user.delete", map[string]any{"id": u.ID, "username": u.Username})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/auth/totp/setup — step 2 for a site account without 2FA:
// a new secret to add to an authenticator app. It is kept (encrypted) on
// the pending session until confirmed; calling again replaces it.
func (s *server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	u := si.User
	if len(u.TOTPSecretEnc) != 0 || u.IsAdmin() {
		writeError(w, http.StatusConflict, "totp_set_up", "two-factor authentication is already set up")
		return
	}
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
	if err := s.Store.SetSessionTOTPSetup(r.Context(), si.Session.TokenHash, enc); err != nil {
		s.internalError(w, "save totp setup", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"secret": enr.Secret, "uri": enr.URL})
}

// POST /api/v1/auth/totp/setup/confirm {code, remember} — a code from the
// app proves it was added; the secret is saved and the login completes.
func (s *server) handleTOTPSetupConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Remember bool   `json:"remember"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	si, _ := sessionFrom(ctx)
	u := si.User
	if len(u.TOTPSecretEnc) != 0 || u.IsAdmin() {
		writeError(w, http.StatusConflict, "totp_set_up", "two-factor authentication is already set up")
		return
	}
	if len(si.Session.TOTPSetupEnc) == 0 {
		writeError(w, http.StatusBadRequest, "totp_setup_missing", "start the two-factor setup first")
		return
	}
	if s.Now().Before(u.LockedUntil) {
		s.Store.DeleteSession(ctx, si.Session.TokenHash)
		s.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", invalidCreds)
		return
	}
	secret, err := s.Keyring.Open(si.Session.TOTPSetupEnc, []byte(u.Username))
	if err != nil {
		s.internalError(w, "decrypt totp setup", err)
		return
	}
	step, ok := auth.CheckTOTP(string(secret), req.Code, s.Now(), 0)
	if !ok {
		if locked := s.recordFailure(r, u); locked {
			s.Store.DeleteSession(ctx, si.Session.TokenHash)
			s.clearSessionCookie(w)
		}
		writeError(w, http.StatusUnauthorized, "invalid_code", "that code didn't match; check the device clock and try again")
		return
	}
	saved, err := s.Store.EnrollTOTP(ctx, u.ID, si.Session.TOTPSetupEnc, step)
	if err != nil {
		s.internalError(w, "enroll totp", err)
		return
	}
	if !saved { // set up meanwhile from another session
		writeError(w, http.StatusConflict, "totp_set_up", "two-factor authentication is already set up")
		return
	}
	s.Log.Info("2fa set up", "user", u.Username)
	s.finishLogin(w, r, si, u, req.Remember)
}

// GET /api/v1/quota — whether disk quotas are on for /home's filesystem.
func (s *server) handleQuotaStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.sites.QuotaStatus(r.Context(), actorOf(r))
	if err != nil {
		s.writeSiteError(w, "quota status", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/v1/quota/enable — turn disk quotas on (installs the quota tools,
// adds usrquota to /etc/fstab, remounts). Admins, once.
func (s *server) handleEnableQuotas(w http.ResponseWriter, r *http.Request) {
	st, err := s.sites.EnableQuotas(r.Context(), actorOf(r))
	if err != nil {
		s.writeSiteError(w, "enable quotas", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GET /api/v1/account/usage — a site login's own usage and limits.
func (s *server) handleAccountUsage(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	if si.User.IsAdmin() {
		writeError(w, http.StatusNotFound, "not_found", "admins have no limits")
		return
	}
	s.writeUsage(w, r, si.User)
}

// GET /api/v1/users/{id}/usage — admins: a site login's usage and limits.
func (s *server) handleUserUsage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	s.writeUsage(w, r, u)
}

func (s *server) writeUsage(w http.ResponseWriter, r *http.Request, u store.User) {
	// ?fresh=1 (the refresh button): measure disk use now, not the cached one.
	usage, err := s.sites.AccountUsage(r.Context(), actorOf(r), u, r.URL.Query().Get("fresh") == "1")
	if err != nil {
		s.writeSiteError(w, "usage", err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// POST /api/v1/quota/disable — turn disk quotas off (and take usrquota out
// of /etc/fstab); every login's disk limit goes back to none.
func (s *server) handleDisableQuotas(w http.ResponseWriter, r *http.Request) {
	st, err := s.sites.DisableQuotas(r.Context(), actorOf(r))
	if err != nil {
		s.writeSiteError(w, "disable quotas", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/v1/users/{id}/password {password} — admins: set a site login's
// password directly. It's signed out everywhere, any lockout and pending
// setup link are cleared; its 2FA stays.
func (s *server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.siteAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internalError(w, "hash password", err)
		return
	}
	if err := s.Store.SetPassword(r.Context(), u.ID, hash); err != nil {
		s.internalError(w, "set password", err)
		return
	}
	// An older setup or reset link must not override the new password.
	if err := s.Store.SetSetupToken(r.Context(), u.ID, "", time.Unix(0, 0)); err != nil {
		s.internalError(w, "clear setup link", err)
		return
	}
	s.auditPanel(r, "user.password_set", map[string]any{"id": u.ID, "username": u.Username})
	w.WriteHeader(http.StatusNoContent)
}
