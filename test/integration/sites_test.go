//go:build integration

// Package integration tests real system actions against a running
// layte-agent on an Ubuntu 24.04 VM. NEVER run this on your own machine.
//
//	make vm-up && make vm-deploy && make test-integration
//
// It builds a test binary on your machine, copies it into the VM and runs it
// as root there.
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

const domain = "layte-it.test"

type env struct {
	svc    *sites.Service
	st     *store.Store
	runner *jobs.Runner
	actor  agent.Actor
}

func setup(t *testing.T) *env {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("run as root inside the test VM")
	}
	sock := os.Getenv("LAYTE_AGENT_SOCKET")
	if sock == "" {
		sock = "/run/layte/agent.sock"
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "it.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	runner := jobs.New(ctx, st, slog.New(slog.NewTextHandler(os.Stderr, nil)), 2)
	ag := &agent.Client{SocketPath: sock}
	return &env{svc: &sites.Service{Store: st, Agent: ag, Jobs: runner}, st: st, runner: runner, actor: agent.Actor{Username: "integration"}}
}

func (e *env) wait(t *testing.T, jobID string) {
	t.Helper()
	e.runner.Wait()
	row, err := e.st.JobByID(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "succeeded" {
		t.Fatalf("job %s %s: %s\nevents: %s", row.Kind, row.Status, row.Error, row.EventsJSON)
	}
}

// foreignChecksums hashes every non-Layte nginx/PHP config, to prove they're untouched.
func foreignChecksums(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pattern := range []string{"/etc/nginx/sites-enabled/*", "/etc/nginx/sites-available/*", "/etc/php/*/fpm/pool.d/*"} {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if strings.Contains(filepath.Base(f), "layte-") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			sum := sha256.Sum256(b)
			out[f] = hex.EncodeToString(sum[:])
		}
	}
	return out
}

func get(t *testing.T, host string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://127.0.0.1/", nil)
	req.Host = host
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.Status + "\n" + string(b)
}

func nginxTestOK(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("/usr/sbin/nginx", "-t").CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %s", out)
	}
}

func phpVersions(t *testing.T) []string {
	dirs, _ := filepath.Glob("/etc/php/*/fpm")
	var out []string
	for _, d := range dirs {
		out = append(out, filepath.Base(filepath.Dir(d)))
	}
	if len(out) == 0 {
		t.Skip("no PHP-FPM installed in the VM")
	}
	return out
}

func TestWordPressSiteLifecycle(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	before := foreignChecksums(t)
	versions := phpVersions(t)

	jobID, siteID, err := e.svc.Create(ctx, e.actor, sites.CreateRequest{
		Domain: domain, PHPVersion: versions[len(versions)-1],
		WordPress: &sites.WordPressFields{Title: "Integration", AdminUser: "itadmin", AdminEmail: "it@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	nginxTestOK(t)

	if body := get(t, domain); !strings.Contains(body, "wp-") && !strings.Contains(body, "WordPress") {
		t.Errorf("site doesn't serve WordPress:\n%.500s", body)
	}
	if _, err := os.Stat("/home/layteittest/" + domain + "/public/wp-config.php"); err != nil {
		t.Error(err)
	}

	site, _ := e.st.SiteByID(ctx, siteID)
	if len(versions) > 1 {
		t.Run("switch PHP", func(t *testing.T) {
			testSwitchPHP(t, e, site, versions[0])
		})
		site, _ = e.st.SiteByID(ctx, siteID)
	}

	delJob, err := e.svc.Delete(ctx, e.actor, site, sites.DeleteRequest{ConfirmDomain: domain, RemoveFiles: true, DropDB: true})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, delJob)
	nginxTestOK(t)
	if _, err := os.Stat("/home/layteittest"); !os.IsNotExist(err) {
		t.Error("home directory still exists")
	}
	if out, _ := exec.Command("/usr/bin/id", "layteittest").CombinedOutput(); !strings.Contains(string(out), "no such user") {
		t.Errorf("user still exists: %s", out)
	}
	if out, _ := exec.Command("/usr/bin/mysql", "--batch", "-e", "SHOW DATABASES LIKE 'layteittest'").CombinedOutput(); strings.Contains(string(out), "layteittest") {
		t.Error("database still exists")
	}

	after := foreignChecksums(t)
	for f, sum := range before {
		if after[f] != sum {
			t.Errorf("foreign config changed: %s", f)
		}
	}
}
