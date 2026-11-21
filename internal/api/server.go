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
	sites       *sites.Service
}

// New builds the full HTTP handler: routes wrapped in middleware.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		Deps:        d,
		authLimiter: auth.NewRateLimiter(5, 10), // per IP: 5/min, burst 10
		sites:       &sites.Service{Store: d.Store, Agent: d.Agent, Jobs: d.Jobs},
	}
	mux := http.NewServeMux()

	// Go 1.22+ patterns: "METHOD /path". A wrong method gets 405 automatically.
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	mux.Handle("POST /api/v1/auth/login", s.rateLimited(http.HandlerFunc(s.handleLogin)))
	mux.Handle("POST /api/v1/auth/totp", s.rateLimited(s.requirePending(http.HandlerFunc(s.handleTOTP))))
	mux.Handle("POST /api/v1/auth/logout", s.requireAnySession(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))

	// authed wraps a handler so it needs a fully logged-in session.
	authed := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }

	mux.Handle("GET /api/v1/dashboard", authed(s.handleDashboard))

	mux.Handle("GET /api/v1/sites", authed(s.handleListSites))
	mux.Handle("POST /api/v1/sites", authed(s.handleCreateSite))
	mux.Handle("DELETE /api/v1/sites/{id}", authed(s.handleDeleteSite))
	mux.Handle("POST /api/v1/sites/{id}/php", authed(s.handleSwitchPHP))
	mux.Handle("POST /api/v1/sites/{id}/ssl", authed(s.handleIssueSSL))
	mux.Handle("DELETE /api/v1/sites/{id}/ssl", authed(s.handleRemoveSSL))
	mux.Handle("GET /api/v1/ssl", authed(s.handleListSSL))

	mux.Handle("GET /api/v1/sites/{id}/files", authed(s.handleListFiles))
	mux.Handle("GET /api/v1/sites/{id}/files/content", authed(s.handleReadFile))
	mux.Handle("PUT /api/v1/sites/{id}/files/content", authed(s.handleSaveFile))
	mux.Handle("GET /api/v1/sites/{id}/files/download", authed(s.handleDownload))
	mux.Handle("POST /api/v1/sites/{id}/files/upload", authed(s.handleUpload))
	mux.Handle("POST /api/v1/sites/{id}/files/{op}", authed(s.handleFileOp))

	mux.Handle("GET /api/v1/security", authed(s.handleSecurity))
	mux.Handle("POST /api/v1/security/ufw/rules", authed(s.handleUFWAllow))
	mux.Handle("DELETE /api/v1/security/ufw/rules/{num}", authed(s.handleUFWDelete))
	mux.Handle("POST /api/v1/security/ufw/enabled", authed(s.handleUFWEnable))
	mux.Handle("POST /api/v1/security/updates", authed(s.handleInstallUpdates))

	mux.Handle("GET /api/v1/logs", authed(s.handleLogSources))
	mux.Handle("GET /api/v1/logs/stream", authed(s.handleLogStream))

	mux.Handle("GET /api/v1/cron", authed(s.handleListCron))
	mux.Handle("POST /api/v1/sites/{id}/cron", authed(s.handleAddCron))
	mux.Handle("PATCH /api/v1/cron/{id}", authed(s.handleUpdateCron))
	mux.Handle("DELETE /api/v1/cron/{id}", authed(s.handleDeleteCron))

	mux.Handle("GET /api/v1/databases", authed(s.handleListDatabases))
	mux.Handle("POST /api/v1/databases", authed(s.handleCreateDatabase))
	mux.Handle("DELETE /api/v1/databases/{name}", authed(s.handleDropDatabase))
	mux.Handle("POST /api/v1/databases/{name}/password", authed(s.handleResetDBPassword))
	mux.Handle("POST /api/v1/sites/scan", authed(s.handleScanSites))
	mux.Handle("GET /api/v1/sites/{id}", authed(s.handleGetSite))
	mux.Handle("GET /api/v1/sites/{id}/vhost", authed(s.handleSiteVhost))
	mux.Handle("GET /api/v1/php/versions", authed(s.handlePHPVersions))

	mux.Handle("GET /api/v1/jobs/{id}", authed(s.handleGetJob))
	mux.Handle("GET /api/v1/jobs/{id}/events", authed(s.handleJobEvents))
	mux.Handle("GET /api/v1/jobs/{id}/secret", authed(s.handleJobSecret))

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
		s.loadSession,
		s.csrf,
	)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}
