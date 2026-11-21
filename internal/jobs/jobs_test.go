package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/store"
)

func newRunner(t *testing.T, max int) (*Runner, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(context.Background(), st, slog.New(slog.NewTextHandler(io.Discard, nil)), max), st
}

func TestJobLifecycle(t *testing.T) {
	r, st := newRunner(t, 2)
	release := make(chan struct{})
	id, err := r.Start("site.create", 0, 1, func(ctx context.Context, j *Job) error {
		j.Step("Create user")
		j.Log("useradd acme")
		<-release
		j.Step("Write vhost")
		j.SetResult(map[string]string{"domain": "a.test"})
		j.SetSecret(map[string]string{"db_password": "s3cret"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Subscribe while running: history so far + live events until done.
	var history []Event
	var ch <-chan Event
	for i := 0; i < 100; i++ {
		var ok bool
		history, ch, _, ok = r.Subscribe(id)
		if ok && len(history) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(history) < 2 || history[0].Step != "Create user" {
		t.Fatalf("history = %+v", history)
	}
	close(release)
	var live []Event
	for e := range ch { // the channel is closed when the job finishes
		live = append(live, e)
	}
	r.Wait()
	last := live[len(live)-1]
	if last.Type != "done" || last.Status != "succeeded" {
		t.Errorf("last event = %+v", last)
	}

	row, err := st.JobByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	json.Unmarshal([]byte(row.EventsJSON), &events)
	if row.Status != "succeeded" || len(events) != len(history)+len(live) || row.ResultJSON != `{"domain":"a.test"}` {
		t.Errorf("row = %+v (%d events)", row, len(events))
	}

	// Secrets: only the starting user, only once, never in SQLite.
	if _, ok := r.TakeSecret(id, 2); ok {
		t.Error("another user got the secret")
	}
	if s, ok := r.TakeSecret(id, 1); !ok || s["db_password"] != "s3cret" {
		t.Errorf("secret = %v %v", s, ok)
	}
	if _, ok := r.TakeSecret(id, 1); ok {
		t.Error("secret readable twice")
	}
	if strings.Contains(row.EventsJSON+row.ResultJSON+row.Error, "s3cret") {
		t.Error("secret persisted")
	}
}

func TestJobFailureAndPanic(t *testing.T) {
	r, st := newRunner(t, 2)
	failID, _ := r.Start("x", 0, 1, func(ctx context.Context, j *Job) error {
		j.Step("Boom")
		return errors.New("nginx -t failed")
	})
	panicID, _ := r.Start("x", 0, 1, func(ctx context.Context, j *Job) error { panic("oops") })
	r.Wait()

	for id, want := range map[string]string{failID: "nginx -t failed", panicID: "internal error: oops"} {
		row, _ := st.JobByID(context.Background(), id)
		if row.Status != "failed" || row.Error != want {
			t.Errorf("job %s: status=%s err=%q", id, row.Status, row.Error)
		}
	}
	row, _ := st.JobByID(context.Background(), failID)
	var events []Event
	json.Unmarshal([]byte(row.EventsJSON), &events)
	if events[len(events)-2].Status != "failed" {
		t.Errorf("failed step not marked: %+v", events)
	}
}

func TestSiteBusy(t *testing.T) {
	r, _ := newRunner(t, 2)
	block := make(chan struct{})
	if _, err := r.Start("x", 7, 1, func(ctx context.Context, j *Job) error { <-block; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start("y", 7, 1, func(ctx context.Context, j *Job) error { return nil }); !errors.Is(err, ErrSiteBusy) {
		t.Errorf("err = %v, want ErrSiteBusy", err)
	}
	close(block)
	r.Wait()
	if _, err := r.Start("z", 7, 1, func(ctx context.Context, j *Job) error { return nil }); err != nil {
		t.Errorf("site still busy after job finished: %v", err)
	}
	r.Wait()
}

func TestConcurrencyLimit(t *testing.T) {
	r, _ := newRunner(t, 1)
	running := make(chan struct{}, 2)
	release := make(chan struct{})
	work := func(ctx context.Context, j *Job) error {
		running <- struct{}{}
		<-release
		return nil
	}
	r.Start("a", 0, 1, work)
	r.Start("b", 0, 1, work)
	<-running
	select {
	case <-running:
		t.Error("two jobs ran at once with limit 1")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	r.Wait()
}

func TestFailInterruptedJobs(t *testing.T) {
	_, st := newRunner(t, 1)
	ctx := context.Background()
	st.CreateJob(ctx, store.JobRow{ID: "old", Kind: "x", CreatedAt: time.Now()})
	if n, err := st.FailInterruptedJobs(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	row, _ := st.JobByID(ctx, "old")
	if row.Status != "failed" {
		t.Errorf("status = %s", row.Status)
	}
}
