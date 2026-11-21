package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/jobs"
)

func TestJobEventsSSE(t *testing.T) {
	env := newTestEnv(t)
	c := env.loggedIn(t)
	userID := int64(1)

	release := make(chan struct{})
	id, err := env.jobs.Start("test", 0, userID, func(ctx context.Context, j *jobs.Job) error {
		j.Step("first")
		<-release
		j.Step("second")
		j.SetSecret(map[string]string{"db_password": "abc"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", env.srv.URL+"/api/v1/jobs/"+id+"/events", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	time.AfterFunc(50*time.Millisecond, func() { close(release) })

	var types []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "event: ") {
			types = append(types, strings.TrimPrefix(line, "event: "))
		}
	}
	got := strings.Join(types, ",")
	if got != "step,step,step,step,done" { // first running, first done, second running, second done, done
		t.Errorf("events = %s", got)
	}

	// Finished job: the stream replays from the store and ends.
	st, body := c.do("GET", "/api/v1/jobs/"+id, nil)
	if st != 200 || body["status"] != "succeeded" || body["has_secret"] != true {
		t.Fatalf("job: %d %v", st, body)
	}
	// Secret: once.
	if st, body := c.do("GET", "/api/v1/jobs/"+id+"/secret", nil); st != 200 || body["secrets"].(map[string]any)["db_password"] != "abc" {
		t.Fatalf("secret: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/jobs/"+id+"/secret", nil); st != 404 {
		t.Errorf("second secret read: %d", st)
	}
	if st, _ := c.do("GET", "/api/v1/jobs/nope", nil); st != 404 {
		t.Errorf("unknown job: %d", st)
	}
}

func TestJobEndpointsNeedAuth(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	for _, p := range []string{"/api/v1/jobs/x", "/api/v1/jobs/x/events", "/api/v1/jobs/x/secret"} {
		if st, _ := c.do("GET", p, nil); st != 401 {
			t.Errorf("%s: %d", p, st)
		}
	}
}
