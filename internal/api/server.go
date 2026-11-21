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
}

// New builds the full HTTP handler: routes wrapped in middleware.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		Deps:        d,
		authLimiter: auth.NewRateLimiter(5, 10), // per IP: 5/min, burst 10
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
