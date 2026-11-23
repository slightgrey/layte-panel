package api

import (
	"context"
	"net/http"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

// GET /api/v1/system/processes — CPU and memory per process and per group
// (service, PHP pool), measured over half a second. Session only.
func (s *server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	var out actions.ProcessesResult
	if err := s.Agent.Call(r.Context(), "system.processes", actorOf(r), nil, &out); err != nil {
		e := s.agentError("system.processes", err)
		writeError(w, http.StatusBadGateway, e.Code, e.Message)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/system/disk?path=/&mode= — sizes of a folder's subfolders on
// its filesystem (mode=files: its largest files). Walking a big disk can take
// a while, so this gets the agent action's own time limit. Session only.
func (s *server) handleDiskBreakdown(w http.ResponseWriter, r *http.Request) {
	p := actions.DiskBreakdownParams{Path: r.URL.Query().Get("path"), Mode: r.URL.Query().Get("mode")}
	if p.Path == "" {
		p.Path = "/"
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute+30*time.Second)
	defer cancel()
	var out actions.DiskBreakdown
	if err := s.Agent.Call(ctx, "system.disk_breakdown", actorOf(r), p, &out); err != nil {
		e := s.agentError("system.disk_breakdown", err)
		status := http.StatusBadGateway
		if e.Code == agent.CodeNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, e.Code, e.Message)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
