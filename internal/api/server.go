// Package api is the HTTP layer of `layte serve`: JSON endpoints under /api/v1
// plus (later) the embedded web UI.
package api

import (
	"log/slog"
	"net/http"

	"layte-panel/internal/config"
	"layte-panel/internal/store"
)

// Deps are the things handlers need. Passing them in a struct (instead of
// globals) keeps handlers easy to test.
type Deps struct {
	Config  config.Config
	Store   *store.Store
	Log     *slog.Logger
	Version string
}

type server struct {
	Deps
}

// New builds the full HTTP handler: routes wrapped in middleware.
func New(d Deps) http.Handler {
	s := &server{Deps: d}
	mux := http.NewServeMux()

	// Go 1.22+ patterns: "METHOD /path". A wrong method gets 405 automatically.
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	return chain(mux,
		s.recoverPanics,
		s.logRequests,
		s.securityHeaders,
	)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}
