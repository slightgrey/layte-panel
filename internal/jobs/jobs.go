// Package jobs runs long operations (create site, delete site, switch PHP) in
// the background. A job gets an ID right away; its progress is a list of
// events that clients can stream over SSE, and its final state is stored in
// SQLite.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/slightgrey/layte-panel/internal/store"
)

// ErrSiteBusy means another job is already running for the same site.
var ErrSiteBusy = errors.New("another job is already running for this site")

// Event is one progress update.
type Event struct {
	Seq     int       `json:"seq"`
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`             // step | log | done
	Step    string    `json:"step,omitempty"`   // for type=step
	Status  string    `json:"status,omitempty"` // step: running|done|failed; done: succeeded|failed
	Message string    `json:"message,omitempty"`
}

// Func is the work of a job. It reports progress through j and returns an
// error to fail the job.
type Func func(ctx context.Context, j *Job) error

// secretTTL: one-time secrets that nobody picks up are dropped after this.
const secretTTL = time.Hour

// Runner starts and tracks jobs.
type Runner struct {
	ctx context.Context
	st  *store.Store
	log *slog.Logger

	// sem is a buffered channel used as a semaphore: sending takes a slot,
	// receiving frees it. With capacity 2, at most two jobs do work at once
	// (enough for a 1 vCPU / 1 GB server); others wait their turn.
	sem chan struct{}

	mu      sync.Mutex
	live    map[string]*Job  // running jobs
	busy    map[int64]string // site id → job id
	secrets map[string]secret
	wg      sync.WaitGroup
}

type secret struct {
	userID  int64
	data    map[string]string
	expires time.Time
}

// New creates a runner. Jobs receive ctx, so cancelling it (on shutdown)
// interrupts them.
func New(ctx context.Context, st *store.Store, log *slog.Logger, maxConcurrent int) *Runner {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Runner{
		ctx: ctx, st: st, log: log,
		sem:     make(chan struct{}, maxConcurrent),
		live:    map[string]*Job{},
		busy:    map[int64]string{},
		secrets: map[string]secret{},
	}
}

// Start creates a job and runs fn in a new goroutine. It returns at once.
// siteID may be 0 for jobs not tied to a site.
func (r *Runner) Start(kind string, siteID, userID int64, fn Func) (string, error) {
	id := newID()
	r.mu.Lock()
	if siteID > 0 {
		if _, ok := r.busy[siteID]; ok {
			r.mu.Unlock()
			return "", ErrSiteBusy
		}
		r.busy[siteID] = id
	}
	j := &Job{ID: id, Kind: kind, SiteID: siteID, UserID: userID, r: r, subs: map[chan Event]struct{}{}}
	r.live[id] = j
	r.mu.Unlock()

	if err := r.st.CreateJob(r.ctx, store.JobRow{ID: id, Kind: kind, SiteID: siteID, UserID: userID, CreatedAt: time.Now()}); err != nil {
		r.release(j)
		return "", err
	}
	r.wg.Add(1)
	go r.run(j, fn)
	return id, nil
}

func (r *Runner) run(j *Job, fn Func) {
	defer r.wg.Done()
	defer r.release(j)

	// Wait for a free slot (or shutdown). `select` waits on several channel
	// operations and runs whichever is ready first.
	select {
	case r.sem <- struct{}{}:
	default:
		j.Log("Waiting for another job to finish…")
		select {
		case r.sem <- struct{}{}:
		case <-r.ctx.Done():
			j.finish(errors.New("interrupted: server shutting down"))
			return
		}
	}
	defer func() { <-r.sem }()

	err := func() (err error) {
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("internal error: %v", v)
				r.log.Error("job panicked", "job", j.ID, "kind", j.Kind, "panic", v)
			}
		}()
		return fn(r.ctx, j)
	}()
	if err != nil && r.ctx.Err() != nil {
		err = fmt.Errorf("interrupted: %w", err)
	}
	j.finish(err)
}

func (r *Runner) release(j *Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, j.ID)
	if j.SiteID > 0 && r.busy[j.SiteID] == j.ID {
		delete(r.busy, j.SiteID)
	}
}

// Wait blocks until all running jobs have finished (used on shutdown and in tests).
func (r *Runner) Wait() { r.wg.Wait() }

// Subscribe returns the events so far and, for a running job, a channel that
// receives new events and is closed when the job finishes. Call cancel when
// done listening. ok is false if the job isn't running (read it from the
// store instead).
func (r *Runner) Subscribe(id string) (history []Event, ch <-chan Event, cancel func(), ok bool) {
	r.mu.Lock()
	j := r.live[id]
	r.mu.Unlock()
	if j == nil {
		return nil, nil, func() {}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	history = append([]Event{}, j.events...)
	if j.done {
		return history, nil, func() {}, true
	}
	c := make(chan Event, 256)
	j.subs[c] = struct{}{}
	return history, c, func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if _, ok := j.subs[c]; ok {
			delete(j.subs, c)
			close(c)
		}
	}, true
}

// TakeSecret returns a job's one-time secrets to the user who started it and
// deletes them, so they can be read exactly once.
func (r *Runner) TakeSecret(jobID string, userID int64) (map[string]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.secrets[jobID]
	if !ok || s.userID != userID || time.Now().After(s.expires) {
		return nil, false
	}
	delete(r.secrets, jobID)
	return s.data, true
}

// HasSecret reports whether a job still has unclaimed secrets for userID.
func (r *Runner) HasSecret(jobID string, userID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.secrets[jobID]
	return ok && s.userID == userID && time.Now().Before(s.expires)
}

// Job is a running job. Its methods are safe to call from the job's goroutine
// while SSE handlers read events from other goroutines.
type Job struct {
	ID     string
	Kind   string
	SiteID int64
	UserID int64

	r       *Runner
	mu      sync.Mutex
	events  []Event
	subs    map[chan Event]struct{}
	current string // running step
	result  any
	done    bool
}

// Step marks the previous step done and starts a new one.
func (j *Job) Step(name string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.current != "" {
		j.emit(Event{Type: "step", Step: j.current, Status: "done"})
	}
	j.current = name
	j.emit(Event{Type: "step", Step: name, Status: "running"})
}

// FailStep marks the running step as failed (e.g. before rolling back).
func (j *Job) FailStep() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.current != "" {
		j.emit(Event{Type: "step", Step: j.current, Status: "failed"})
		j.current = ""
	}
}

// Log adds a progress message.
func (j *Job) Log(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.emit(Event{Type: "log", Message: fmt.Sprintf(format, args...)})
}

// SetResult stores a (non-secret) result, saved with the job.
func (j *Job) SetResult(v any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.result = v
}

// SetSecret keeps secrets (generated passwords) in memory only, for one read
// by the job's user via Runner.TakeSecret. They are never written to SQLite.
func (j *Job) SetSecret(data map[string]string) {
	j.r.mu.Lock()
	defer j.r.mu.Unlock()
	j.r.secrets[j.ID] = secret{userID: j.UserID, data: data, expires: time.Now().Add(secretTTL)}
}

func (j *Job) finish(err error) {
	j.mu.Lock()
	status, msg := "succeeded", ""
	if err != nil {
		status, msg = "failed", err.Error()
	}
	if j.current != "" {
		stepStatus := "done"
		if err != nil {
			stepStatus = "failed"
		}
		j.emit(Event{Type: "step", Step: j.current, Status: stepStatus})
	}
	j.emit(Event{Type: "done", Status: status, Message: msg})
	j.done = true
	events := store.ToJSON(j.events)
	result := store.ToJSON(j.result)
	for c := range j.subs {
		close(c)
	}
	j.subs = map[chan Event]struct{}{}
	j.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(j.r.ctx), 5*time.Second)
	defer cancel()
	if err := j.r.st.FinishJob(ctx, j.ID, status, events, msg, result, time.Now()); err != nil {
		j.r.log.Error("save finished job", "job", j.ID, "err", err)
	}
	if status == "failed" {
		j.r.log.Warn("job failed", "job", j.ID, "kind", j.Kind, "err", msg)
	} else {
		j.r.log.Info("job succeeded", "job", j.ID, "kind", j.Kind)
	}
}

// emit appends an event, persists the log and fans it out. Caller holds j.mu.
func (j *Job) emit(e Event) {
	e.Seq = len(j.events) + 1
	e.Time = time.Now().UTC()
	j.events = append(j.events, e)
	for c := range j.subs {
		select {
		case c <- e: // delivered
		default:
			// Subscriber isn't keeping up: drop it. EventSource reconnects and
			// replays the history, so nothing is lost.
			delete(j.subs, c)
			close(c)
		}
	}
	if e.Type != "done" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(j.r.ctx), 2*time.Second)
		defer cancel()
		j.r.st.SaveJobEvents(ctx, j.ID, store.ToJSON(j.events))
	}
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
