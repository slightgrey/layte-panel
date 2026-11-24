package api

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

func TestLogSources(t *testing.T) {
	_, c, deps := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "logs.test", "php_version": "8.3"})
	waitJob(t, c, b["job_id"].(string))
	deps.FS.WriteFileAtomic("/home/logstest/logs.test/logs/error.log", []byte("x\n"), 0o640)

	_, body := c.do("GET", "/api/v1/logs", nil)
	paths := map[string]map[string]any{}
	for _, x := range body["sources"].([]any) {
		s := x.(map[string]any)
		paths[s["path"].(string)] = s
	}
	for _, want := range []string{"/var/log/nginx/access.log", "/var/log/php8.3-fpm.log", "/var/log/nginx/shop.access.log", "/home/logstest/logs.test/logs/error.log"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("missing source %s (got %v)", want, paths)
		}
	}
	if s := paths["/var/log/nginx/shop.access.log"]; s["domain"] != "shop.example.org" || s["site_id"] == nil {
		t.Errorf("shop log not linked to its site: %v", s)
	}
}

func TestLogStream(t *testing.T) {
	env, c, deps := siteEnv(t)
	u := env.srv.URL + "/api/v1/logs/stream?path=" + url.QueryEscape("/var/log/nginx/access.log") + "&lines=2"
	req, _ := http.NewRequest("GET", u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	next := func() (string, string) {
		var event string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				return event, strings.TrimPrefix(line, "data: ")
			}
		}
		return "", ""
	}
	ev, data := next()
	if ev != "tail" || strings.Count(data, `\n`) != 2 || !strings.Contains(data, "POST /wp-login.php") {
		t.Fatalf("tail: %s %s", ev, data)
	}

	old, _ := deps.FS.ReadFile("/var/log/nginx/access.log")
	deps.FS.WriteFileAtomic("/var/log/nginx/access.log", append(old, []byte("10.0.0.1 - - new line\n")...), 0o640)
	done := make(chan struct{})
	go func() {
		ev, data = next()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no append event")
	}
	if ev != "append" || !strings.Contains(data, "new line") || strings.Contains(data, "wp-login") {
		t.Errorf("append: %s %s", ev, data)
	}
}

// A graceful shutdown doesn't wait for open log streams.
func TestLogStreamEndsOnShutdown(t *testing.T) {
	env, c, _ := siteEnv(t)
	u := env.srv.URL + "/api/v1/logs/stream?path=" + url.QueryEscape("/var/log/nginx/access.log") + "&lines=2"
	resp, err := c.http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !bufio.NewScanner(resp.Body).Scan() {
		t.Fatal("stream didn't start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := env.srv.Config.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v after %s", err, time.Since(start))
	}
}

func TestLogStreamRejectsOtherFiles(t *testing.T) {
	_, c, _ := siteEnv(t)
	for _, p := range []string{"/etc/shadow", "/var/log/nginx/../../../etc/shadow", "/var/log/auth.log"} {
		if st, _ := c.do("GET", "/api/v1/logs/stream?path="+url.QueryEscape(p), nil); st != 404 {
			t.Errorf("%s: %d", p, st)
		}
	}
}

func TestLogReadAllowlist(t *testing.T) {
	for _, bad := range []string{"/etc/shadow", "/var/log/nginx/../x.log", "/home/root/../etc/logs/x.log", "/var/log/syslog"} {
		if (actions.LogReadParams{Path: bad}).Validate() == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	// A symlink inside an allowed folder pointing elsewhere is refused by the agent.
	ag, deps := devAgent(t)
	deps.FS.Symlink("/etc/passwd", "/var/log/nginx/evil.log")
	err := ag.Call(t.Context(), "logs.read", agent.Actor{}, actions.LogReadParams{Path: "/var/log/nginx/evil.log", Offset: -1}, nil)
	if err == nil {
		t.Error("followed a symlink out of the log folders")
	}
}
