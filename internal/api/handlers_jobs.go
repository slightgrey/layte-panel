package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
)

type jobJSON struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	SiteID     int64           `json:"site_id,omitempty"`
	Status     string          `json:"status"`
	Error      string          `json:"error,omitempty"`
	Events     json.RawMessage `json:"events"`
	Result     json.RawMessage `json:"result"`
	HasSecret  bool            `json:"has_secret"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// GET /api/v1/jobs/{id}
func (s *server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	row, err := s.Store.JobByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such job")
		return
	}
	if err != nil {
		s.internalError(w, "get job", err)
		return
	}
	if !s.ownsJob(r, row) {
		writeError(w, http.StatusNotFound, "not_found", "no such job")
		return
	}
	out := jobJSON{
		ID: row.ID, Kind: row.Kind, SiteID: row.SiteID, Status: row.Status, Error: row.Error,
		Events: json.RawMessage(row.EventsJSON), Result: json.RawMessage(row.ResultJSON),
		HasSecret: s.Jobs.HasSecret(row.ID, si.User.ID), CreatedAt: row.CreatedAt,
	}
	if !row.FinishedAt.IsZero() {
		out.FinishedAt = &row.FinishedAt
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/jobs/{id}/secret — one-time credentials (generated passwords).
// Only the admin who started the job can read them, and only once.
func (s *server) handleJobSecret(w http.ResponseWriter, r *http.Request) {
	si, _ := sessionFrom(r.Context())
	sec, ok := s.Jobs.TakeSecret(r.PathValue("id"), si.User.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "secret_gone", "these credentials were already shown, or have expired")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"secrets": sec})
}

// sseHeartbeat keeps proxies from closing an idle stream.
const sseHeartbeat = 15 * time.Second

// GET /api/v1/jobs/{id}/events — Server-Sent Events: the job's history, then
// live events until it finishes. EventSource sends Last-Event-ID when it
// reconnects, so already-seen events are skipped.
func (s *server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}
	lastSeen, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	if isSiteUser(r) { // site accounts: only the jobs they started
		row, err := s.Store.JobByID(r.Context(), id)
		if err != nil || !s.ownsJob(r, row) {
			writeError(w, http.StatusNotFound, "not_found", "no such job")
			return
		}
	}

	history, live, cancel, running := s.Jobs.Subscribe(id)
	defer cancel()
	if !running {
		row, err := s.Store.JobByID(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such job")
			return
		}
		if err != nil {
			s.internalError(w, "get job", err)
			return
		}
		json.Unmarshal([]byte(row.EventsJSON), &history)
		if !containsDone(history) { // e.g. interrupted by a restart
			history = append(history, jobs.Event{Seq: len(history) + 1, Type: "done", Status: row.Status, Message: row.Error})
		}
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // tell nginx not to buffer
	w.WriteHeader(http.StatusOK)

	send := func(e jobs.Event) bool {
		if e.Seq <= lastSeen {
			return true
		}
		b, _ := json.Marshal(e)
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, e := range history {
		if !send(e) {
			return
		}
	}
	if live == nil {
		return
	}

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-live:
			if !ok {
				return // job finished (its done event was already sent)
			}
			if !send(e) {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return // client went away
		case <-stopping(r.Context()):
			return // server is shutting down
		}
	}
}

func containsDone(events []jobs.Event) bool {
	for _, e := range events {
		if e.Type == "done" {
			return true
		}
	}
	return false
}
