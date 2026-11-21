package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
)

type logSourceJSON struct {
	actions.LogSource
	SiteID int64 `json:"site_id,omitempty"`
}

// logSources asks the agent which log files exist and links them to sites.
func (s *server) logSources(r *http.Request) ([]logSourceJSON, error) {
	var srcs []actions.LogSource
	if err := s.Agent.Call(r.Context(), "logs.sources", actorOf(r), nil, &srcs); err != nil {
		return nil, err
	}
	sites, err := s.Store.ListSites(r.Context())
	if err != nil {
		return nil, err
	}
	ids := map[string]int64{}
	for _, x := range sites {
		ids[x.Domain] = x.ID
	}
	out := make([]logSourceJSON, 0, len(srcs))
	for _, src := range srcs {
		out = append(out, logSourceJSON{LogSource: src, SiteID: ids[src.Domain]})
	}
	return out, nil
}

// GET /api/v1/logs
func (s *server) handleLogSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.logSources(r)
	if err != nil {
		s.writeSiteError(w, "log sources", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": srcs})
}

// logPoll is how often a live tail checks for new lines.
const logPoll = time.Second

// GET /api/v1/logs/stream?path=…&lines=200 — Server-Sent Events: a "tail"
// event with the last lines, then "append" events as the file grows.
func (s *server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	srcs, err := s.logSources(r)
	if err != nil {
		s.writeSiteError(w, "log sources", err)
		return
	}
	if !slices.ContainsFunc(srcs, func(x logSourceJSON) bool { return x.Path == p }) {
		writeError(w, http.StatusNotFound, "not_found", "unknown log file")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}

	read := func(offset int64) (actions.LogChunk, error) {
		var c actions.LogChunk
		err := s.Agent.Call(r.Context(), "logs.read", actorOf(r), actions.LogReadParams{Path: p, Offset: offset, TailLines: lines}, &c)
		return c, err
	}
	chunk, err := read(-1)
	if err != nil {
		s.writeSiteError(w, "read log", err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send("tail", map[string]any{"data": chunk.Data}) {
		return
	}

	offset := chunk.Offset
	poll := time.NewTicker(logPoll)
	defer poll.Stop()
	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
			c, err := read(offset)
			if err != nil {
				e := s.agentError("logs.read", err)
				send("error", e)
				return
			}
			offset = c.Offset
			if c.Data != "" || c.Rotated {
				if !send("append", map[string]any{"data": c.Data, "rotated": c.Rotated}) {
					return
				}
			}
		}
	}
}
