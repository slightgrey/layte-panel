package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/slightgrey/layte-panel/internal/actions"
	"sync"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
)

// section is one dashboard panel: either data or an error, never both, so
// one failing check (e.g. apt) doesn't blank the whole page.
type section struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error *apiError       `json:"error,omitempty"`
}

// dashboardSections maps response keys to agent actions.
var dashboardSections = map[string]string{
	"system":   "system.info",
	"services": "services.status",
	"versions": "versions.get",
	"updates":  "updates.pending",
}

// GET /api/v1/dashboard
func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	actor := agent.Actor{UserID: si.User.ID, Username: si.User.Username}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Run the four agent calls at the same time. Each goroutine writes only
	// its own key, but maps aren't safe for concurrent writes, so a Mutex
	// guards the map; the WaitGroup waits for all of them to finish.
	out := make(map[string]section, len(dashboardSections))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for key, action := range dashboardSections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var raw json.RawMessage
			sec := section{}
			if err := s.Agent.Call(ctx, action, actor, nil, &raw); err != nil {
				sec.Error = s.agentError(action, err)
			} else {
				sec.Data = raw
			}
			mu.Lock()
			out[key] = sec
			mu.Unlock()
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

// agentError turns an agent failure into a safe API error. Typed agent errors
// keep their code; anything else means the socket couldn't be reached.
func (s *server) agentError(action string, err error) *apiError {
	var ae *agent.Error
	if errors.As(err, &ae) {
		s.Log.Warn("agent action failed", "action", action, "code", ae.Code, "err", ae.Message)
		return &apiError{Code: ae.Code, Message: "the check failed on the server"}
	}
	s.Log.Error("agent unavailable", "action", action, "err", err)
	return &apiError{Code: "agent_unavailable", Message: "cannot reach layte-agent; is the service running?"}
}

// GET /api/v1/system/info — just the server's CPU, memory and disks (the
// sidebar's server stats; lighter than the whole dashboard).
func (s *server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var info actions.SystemInfo
	if err := s.Agent.Call(r.Context(), "system.info", actorOf(r), nil, &info); err != nil {
		s.writeSiteError(w, "system info", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}
