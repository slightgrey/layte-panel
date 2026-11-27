// Package api is the HTTP layer of `layte serve`: JSON endpoints under /api/v1
// plus (later) the embedded web UI.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/config"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

// AgentCaller is the part of agent.Client the API uses. Tests pass a fake.
type AgentCaller interface {
	Call(ctx context.Context, action string, actor agent.Actor, params, out any) error
}

// Deps are the things handlers need. Passing them in a struct (instead of
// globals) keeps handlers easy to test.
type Deps struct {
	Config  config.Config
	Store   *store.Store
	Keyring *auth.Keyring
	Agent   AgentCaller
	Jobs    *jobs.Runner
	Log     *slog.Logger
	Version string
	UI      fs.FS            // built web app (web/dist); nil shows a "not built" page
	Now     func() time.Time // optional; defaults to time.Now (tests override it)
}

type server struct {
	Deps
	authLimiter *auth.RateLimiter
	keyLimiter  *auth.RateLimiter
	sites       *sites.Service
}

// New builds the full HTTP handler: routes wrapped in middleware.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		Deps:        d,
		authLimiter: auth.NewRateLimiter(5, 10),    // per IP: 5/min, burst 10
		keyLimiter:  auth.NewRateLimiter(120, 240), // per API key
		sites:       &sites.Service{Store: d.Store, Agent: d.Agent, Jobs: d.Jobs},
	}
	mux := http.NewServeMux()

	// Go 1.22+ patterns: "METHOD /path". A wrong method gets 405 automatically.
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	mux.Handle("POST /api/v1/auth/login", s.rateLimited(http.HandlerFunc(s.handleLogin)))
	mux.Handle("POST /api/v1/auth/totp", s.rateLimited(s.requirePending(http.HandlerFunc(s.handleTOTP))))
	mux.Handle("POST /api/v1/auth/totp/setup", s.rateLimited(s.requirePending(http.HandlerFunc(s.handleTOTPSetup))))
	mux.Handle("POST /api/v1/auth/totp/setup/confirm", s.rateLimited(s.requirePending(http.HandlerFunc(s.handleTOTPSetupConfirm))))
	mux.Handle("GET /api/v1/setup/{token}", s.rateLimited(http.HandlerFunc(s.handleSetupLink)))
	mux.Handle("POST /api/v1/setup/{token}", s.rateLimited(http.HandlerFunc(s.handleUseSetupLink)))
	mux.Handle("POST /api/v1/auth/logout", s.requireAnySession(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))

	// authed: a logged-in admin only (sessions). Everything that changes
	// something, plus files, jobs and key management, is session-only.
	// This is the default; site accounts only get the routes below that are
	// wrapped in siteAuthed or account (see access.go).
	authed := func(h http.HandlerFunc) http.Handler { return s.requireAdmin(h) }
	// siteAuthed: an admin, or the site account that owns the {id} site.
	siteAuthed := func(h http.HandlerFunc) http.Handler { return s.requireSiteAccess(h) }
	// account: any logged-in user; the handler limits a site account to its
	// own sites (lists are filtered, single items checked).
	account := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	// read: a logged-in admin, or an API key with the scope (GET only).
	// readSite / readAccount: the same, but also open to site accounts, like
	// siteAuthed / account.
	read := func(scope string, h http.HandlerFunc) http.Handler { return s.scoped(scope, s.requireAdmin, h) }
	readSite := func(scope string, h http.HandlerFunc) http.Handler { return s.scoped(scope, s.requireSiteAccess, h) }
	readAccount := func(scope string, h http.HandlerFunc) http.Handler { return s.scoped(scope, s.requireAuth, h) }

	mux.Handle("GET /api/v1/openapi.json", http.HandlerFunc(s.handleOpenAPI))

	// Readable with API keys.
	mux.Handle("GET /api/v1/dashboard", read("system:read", s.handleDashboard))
	mux.Handle("GET /api/v1/sites", readAccount("sites:read", s.handleListSites))
	mux.Handle("GET /api/v1/sites/{id}", readSite("sites:read", s.handleGetSite))
	mux.Handle("GET /api/v1/sites/{id}/vhost", read("sites:read", s.handleSiteVhost))
	mux.Handle("GET /api/v1/sites/{id}/disk", readSite("sites:read", s.handleSiteDisk))
	mux.Handle("GET /api/v1/php/versions", readAccount("sites:read", s.handlePHPVersions))
	mux.Handle("GET /api/v1/ssl", readAccount("ssl:read", s.handleListSSL))
	mux.Handle("GET /api/v1/databases", readAccount("databases:read", s.handleListDatabases))
	mux.Handle("GET /api/v1/cron", readAccount("cron:read", s.handleListCron))
	mux.Handle("GET /api/v1/security", read("security:read", s.handleSecurity))
	mux.Handle("GET /api/v1/logs", readAccount("logs:read", s.handleLogSources))
	mux.Handle("GET /api/v1/logs/stream", readAccount("logs:read", s.handleLogStream))

	// Sites (session only).
	mux.Handle("POST /api/v1/sites", account(s.handleCreateSite))
	mux.Handle("POST /api/v1/sites/scan", authed(s.handleScanSites))
	mux.Handle("DELETE /api/v1/sites/{id}", siteAuthed(s.handleDeleteSite))
	mux.Handle("POST /api/v1/sites/{id}/php", siteAuthed(s.handleSwitchPHP))
	mux.Handle("PUT /api/v1/sites/{id}/php-shell", authed(s.handleSetPHPShell))
	mux.Handle("PUT /api/v1/sites/{id}/php-settings", siteAuthed(s.handleSetPHPSettings))
	mux.Handle("PUT /api/v1/sites/{id}/redirects", siteAuthed(s.handleSetRedirects))
	mux.Handle("GET /api/v1/sites/{id}/sftp", siteAuthed(s.handleSFTP))
	mux.Handle("PUT /api/v1/sites/{id}/sftp", siteAuthed(s.handleSetSFTP))
	mux.Handle("POST /api/v1/sites/{id}/sftp/password", siteAuthed(s.handleSFTPPassword))
	mux.Handle("PUT /api/v1/sites/{id}/page-cache", siteAuthed(s.handleSetPageCache))
	mux.Handle("POST /api/v1/sites/{id}/page-cache/purge", siteAuthed(s.handlePurgeCache))
	mux.Handle("GET /api/v1/sites/{id}/object-cache", siteAuthed(s.handleObjectCache))
	mux.Handle("PUT /api/v1/sites/{id}/object-cache", siteAuthed(s.handleSetObjectCache))
	mux.Handle("POST /api/v1/sites/{id}/object-cache/flush", siteAuthed(s.handleFlushObjectCache))
	mux.Handle("POST /api/v1/sites/{id}/object-cache/resume", siteAuthed(s.handleResumeObjectCache))
	mux.Handle("GET /api/v1/sites/{id}/adopt", authed(s.handleAdoptPreview))
	mux.Handle("POST /api/v1/sites/{id}/adopt", authed(s.handleAdopt))
	mux.Handle("GET /api/v1/sites/{id}/takeover", authed(s.handleTakeoverPreview))
	mux.Handle("POST /api/v1/sites/{id}/takeover", authed(s.handleTakeover))
	mux.Handle("POST /api/v1/sites/{id}/ssl", siteAuthed(s.handleIssueSSL))
	mux.Handle("DELETE /api/v1/sites/{id}/ssl", siteAuthed(s.handleRemoveSSL))
	mux.Handle("POST /api/v1/sites/{id}/ssl/renew", siteAuthed(s.handleRenewSSL))
	mux.Handle("POST /api/v1/sites/{id}/cron", siteAuthed(s.handleAddCron))
	mux.Handle("GET /api/v1/sites/{id}/wp-admin", siteAuthed(s.handleWPAdmin))
	mux.Handle("GET /api/v1/wp-admin.js", account(s.handleWPAdminScript))
	mux.Handle("GET /api/v1/sites/{id}/wp-debug", siteAuthed(s.handleGetWPDebug))
	mux.Handle("PUT /api/v1/sites/{id}/wp-debug", siteAuthed(s.handleSetWPDebug))

	// Files (session only).
	mux.Handle("GET /api/v1/sites/{id}/files", siteAuthed(s.handleListFiles))
	mux.Handle("GET /api/v1/sites/{id}/files/content", siteAuthed(s.handleReadFile))
	mux.Handle("PUT /api/v1/sites/{id}/files/content", siteAuthed(s.handleSaveFile))
	mux.Handle("GET /api/v1/sites/{id}/files/download", siteAuthed(s.handleDownload))
	mux.Handle("POST /api/v1/sites/{id}/files/upload", siteAuthed(s.handleUpload))
	mux.Handle("POST /api/v1/sites/{id}/files/{op}", siteAuthed(s.handleFileOp))

	// Security, cron, databases (session only).
	mux.Handle("POST /api/v1/security/ufw/rules", authed(s.handleUFWAllow))
	mux.Handle("DELETE /api/v1/security/ufw/rules/{num}", authed(s.handleUFWDelete))
	mux.Handle("POST /api/v1/security/ufw/enabled", authed(s.handleUFWEnable))
	mux.Handle("POST /api/v1/security/cloudflare", authed(s.handleCloudflare))
	mux.Handle("POST /api/v1/security/updates", authed(s.handleInstallUpdates))
	mux.Handle("GET /api/v1/updates/plan", authed(s.handleUpdatesPlan))
	mux.Handle("POST /api/v1/updates/refresh", authed(s.handleUpdatesRefresh))
	mux.Handle("POST /api/v1/updates/apply", authed(s.handleUpdatesApply))
	mux.Handle("GET /api/v1/system/processes", authed(s.handleProcesses))
	mux.Handle("GET /api/v1/system/disk", authed(s.handleDiskBreakdown))
	mux.Handle("PATCH /api/v1/cron/{id}", account(s.handleUpdateCron))
	mux.Handle("DELETE /api/v1/cron/{id}", account(s.handleDeleteCron))
	mux.Handle("POST /api/v1/databases", authed(s.handleCreateDatabase))
	mux.Handle("DELETE /api/v1/databases/{name}", authed(s.handleDropDatabase))
	mux.Handle("POST /api/v1/databases/{name}/password", account(s.handleResetDBPassword))
	mux.Handle("POST /api/v1/databases/{name}/export", account(s.handleExportDatabase))
	mux.Handle("POST /api/v1/databases/{name}/import", account(s.handleImportDatabase))
	mux.Handle("POST /api/v1/sites/{id}/databases/link", authed(s.handleLinkDatabase))
	mux.Handle("DELETE /api/v1/databases/{name}/link", authed(s.handleUnlinkDatabase))

	// Jobs and API keys (session only).
	mux.Handle("GET /api/v1/jobs/{id}", account(s.handleGetJob))
	mux.Handle("GET /api/v1/jobs/{id}/events", account(s.handleJobEvents))
	mux.Handle("GET /api/v1/jobs/{id}/secret", account(s.handleJobSecret))
	// The Account page: your own profile, password, 2FA and sessions (any role).
	limitedAccount := func(h http.HandlerFunc) http.Handler { return s.rateLimited(s.requireAuth(h)) }
	mux.Handle("GET /api/v1/account", account(s.handleAccount))
	mux.Handle("PATCH /api/v1/account", account(s.handleUpdateAccount))
	mux.Handle("POST /api/v1/account/password", limitedAccount(s.handleChangePassword))
	mux.Handle("POST /api/v1/account/2fa", limitedAccount(s.handleStart2FAChange))
	mux.Handle("POST /api/v1/account/2fa/confirm", limitedAccount(s.handleConfirm2FAChange))
	mux.Handle("DELETE /api/v1/account/sessions", account(s.handleSignOutOthers))
	mux.Handle("DELETE /api/v1/account/sessions/{id}", account(s.handleSignOutSession))

	mux.Handle("GET /api/v1/users", authed(s.handleListUsers))
	mux.Handle("POST /api/v1/users", authed(s.handleCreateUser))
	mux.Handle("PATCH /api/v1/users/{id}", authed(s.handleUpdateUser))
	mux.Handle("POST /api/v1/users/{id}/password", authed(s.handleResetUserPassword))
	mux.Handle("POST /api/v1/users/{id}/reset-2fa", authed(s.handleResetUser2FA))
	mux.Handle("DELETE /api/v1/users/{id}", authed(s.handleDeleteUser))
	mux.Handle("GET /api/v1/keys", authed(s.handleListKeys))
	mux.Handle("POST /api/v1/keys", authed(s.handleCreateKey))
	mux.Handle("DELETE /api/v1/keys/{id}", authed(s.handleRevokeKey))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	// Everything that isn't /api is the embedded React app.
	mux.Handle("/", spaHandler(d.UI))

	return chain(mux,
		s.recoverPanics,
		s.logRequests,
		s.securityHeaders,
		s.devCORS,
		s.loadAPIKey,
		s.loadSession,
		s.csrf,
	)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}
