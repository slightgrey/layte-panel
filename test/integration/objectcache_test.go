//go:build integration

package integration

import (
	"context"
	_ "embed"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/store"
)

// testObjectCache turns the Redis object cache on for the real WordPress
// site, checks WordPress really uses its own Redis (and nobody else can
// reach it), off and on again, and leaves it on so deleting the site has to
// clean it up.
func testObjectCache(t *testing.T, e *env, site store.Site) {
	ctx := context.Background()
	user := site.SystemUser
	sock := "/run/layte-redis/" + user + "/redis.sock"
	dropIn := "/home/" + user + "/" + site.Domain + "/public/wp-content/object-cache.php"
	// Pages differ between requests (nonces); the status mustn't.
	status := func() string { return strings.SplitN(get(t, site.Domain), "\n", 2)[0] }
	before := status()

	jobID, err := e.svc.SetObjectCache(ctx, e.actor, site, true)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	site, _ = e.st.SiteByID(ctx, site.ID)
	if !site.ObjectCache.Enabled {
		t.Fatal("not enabled after the job")
	}
	if after := status(); after != before {
		t.Errorf("site answers %s with the object cache, %s before", after, before)
	}
	if b, err := os.ReadFile(dropIn); err != nil || !strings.Contains(string(b), "Layte Object Cache") {
		t.Errorf("drop-in: %v", err)
	}
	if _, err := os.Stat("/home/" + user + "/" + site.Domain + "/public/wp-content/plugins/redis-cache"); err == nil {
		t.Error("a third-party plugin was installed")
	}

	// Layte's cache behaves as WordPress expects (WP-CLI, as the site user).
	wp := func(phase string) string {
		t.Helper()
		out, err := exec.Command("sudo", "-u", user, "php"+site.PHPVersion, "/usr/local/lib/layte/wp-cli-2.12.0.phar",
			"--path=/home/"+user+"/"+site.Domain+"/public", "eval-file", checkScript(t), phase).CombinedOutput()
		if err != nil {
			t.Errorf("wp eval-file %s: %v\n%s", phase, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for _, phase := range []string{"api", "plugin", "set", "get", "expire-set"} {
		if out := wp(phase); !strings.HasSuffix(out, "OK") {
			t.Errorf("%s: %s", phase, out)
		}
	}
	time.Sleep(3 * time.Second)
	if out := wp("expire-get"); !strings.HasSuffix(out, "OK") {
		t.Errorf("expire-get: %s", out)
	}

	// Disabled in WordPress (the settings page's flag): no Redis, transients
	// in the database; Layte sees it as paused; Resume turns it back on.
	paused := "/home/" + user + "/" + site.Domain + "/public/wp-content/mu-plugins/layte-object-cache/.paused"
	if err := exec.Command("sudo", "-u", user, "touch", paused).Run(); err != nil {
		t.Fatal(err)
	}
	if out := wp("paused"); !strings.HasSuffix(out, "OK") {
		t.Errorf("paused: %s", out)
	}
	if after := status(); after != before {
		t.Errorf("site answers %s while paused, %s before", after, before)
	}
	if st, _ := e.svc.ObjectCache(ctx, e.actor, site); !st.Paused {
		t.Error("Layte doesn't show the cache as paused")
	}
	if err := e.svc.ResumeObjectCache(ctx, e.actor, site); err != nil {
		t.Fatal(err)
	}
	if out := wp("api"); !strings.HasSuffix(out, "OK") {
		t.Errorf("after resume: %s", out)
	}
	// A page view (through PHP-FPM) stores WordPress's objects in the site's
	// own Redis, under its prefix. (The check above ended with a flush.)
	// The page cache is on: a query string makes PHP run.
	if req, err := http.NewRequest("GET", "http://127.0.0.1/?layte-it="+strconv.FormatInt(time.Now().UnixNano(), 10), nil); err == nil {
		req.Host = site.Domain
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	out, err := exec.Command("redis-cli", "-s", sock, "--scan", "--pattern", user+":*").CombinedOutput()
	if err != nil || strings.Count(string(out), "\n") < 5 {
		t.Errorf("few or no keys under %s: in Redis: %v\n%.300s", user, err, out)
	}
	st, err := e.svc.ObjectCache(ctx, e.actor, site)
	if err != nil || !st.Running || st.Keys == 0 || st.LimitBytes != 32<<20 {
		t.Errorf("status: %+v %v", st, err)
	}
	// Only the site's user can reach it; no TCP port.
	if o, err := exec.Command("sudo", "-u", "nobody", "redis-cli", "-s", sock, "PING").CombinedOutput(); err == nil && strings.Contains(string(o), "PONG") {
		t.Error("another user reached the site's Redis")
	}
	if o, _ := exec.Command("ss", "-ltnp").Output(); strings.Contains(string(o), "redis") {
		t.Errorf("Redis listens on TCP:\n%s", o)
	}
	if o, _ := exec.Command("ps", "-o", "user=", "-C", "redis-server").Output(); !strings.Contains(string(o), user) {
		t.Errorf("Redis doesn't run as %s: %q", user, o)
	}

	// Flush, then off: drop-in and Redis gone, site as before.
	if err := e.svc.FlushObjectCache(ctx, e.actor, site); err != nil {
		t.Error(err)
	}
	jobID, err = e.svc.SetObjectCache(ctx, e.actor, site, false)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	if _, err := os.Stat(dropIn); !os.IsNotExist(err) {
		t.Error("drop-in left after turning the object cache off")
	}
	if o, _ := exec.Command("systemctl", "is-active", "layte-redis@"+user+".service").Output(); strings.TrimSpace(string(o)) == "active" {
		t.Error("Redis still running after turning the object cache off")
	}
	if after := status(); after != before {
		t.Errorf("site answers %s after turning the object cache off, %s before", after, before)
	}

	// A site set up by v0.1.34–35: the Redis Object Cache plugin from
	// wordpress.org, its drop-in and constants, Layte's Redis. Turning the
	// object cache on moves it to Layte's own cache and removes the plugin.
	wpPath := "/home/" + user + "/" + site.Domain + "/public"
	wpcli := func(args ...string) {
		t.Helper()
		a := append([]string{"-u", user, "php" + site.PHPVersion, "/usr/local/lib/layte/wp-cli-2.12.0.phar", "--path=" + wpPath, "--skip-plugins", "--skip-themes"}, args...)
		if out, err := exec.Command("sudo", a...).CombinedOutput(); err != nil {
			t.Fatalf("wp %v: %v\n%s", args, err, out)
		}
	}
	wpcli("plugin", "install", "redis-cache", "--activate")
	wpcli("config", "set", "WP_REDIS_SCHEME", "unix", "--type=constant")
	wpcli("config", "set", "WP_REDIS_PATH", sock, "--type=constant")
	wpcli("config", "set", "WP_REDIS_PREFIX", user+":", "--type=constant")
	if out, err := exec.Command("sudo", "-u", user, "cp", wpPath+"/wp-content/plugins/redis-cache/includes/object-cache.php", dropIn).CombinedOutput(); err != nil {
		t.Fatalf("copy plugin drop-in: %v %s", err, out)
	}
	exec.Command("systemctl", "start", "layte-redis@"+user+".service").Run()
	site, _ = e.st.SiteByID(ctx, site.ID)
	site.ObjectCache = store.ObjectCache{Enabled: true, InstalledPlugin: true}
	e.st.UpdateSite(ctx, site)
	if st, _ := e.svc.ObjectCache(ctx, e.actor, site); st.DropIn != "redis-cache" {
		t.Errorf("old setup not recognised: drop_in %q", st.DropIn)
	}
	jobID, err = e.svc.SetObjectCache(ctx, e.actor, site, true)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	if b, _ := os.ReadFile(dropIn); !strings.Contains(string(b), "Layte Object Cache") {
		t.Error("drop-in not replaced with Layte's")
	}
	if _, err := os.Stat(wpPath + "/wp-content/plugins/redis-cache"); err == nil {
		t.Error("the plugin Layte installed is still there")
	}
	if b, _ := os.ReadFile(wpPath + "/wp-config.php"); strings.Contains(string(b), "WP_REDIS_PATH") {
		t.Error("the plugin's constants are still in wp-config.php")
	}
	if out := wp("api"); !strings.HasSuffix(out, "OK") {
		t.Errorf("after moving off the plugin: %s", out)
	}
	if after := status(); after != before {
		t.Errorf("site answers %s after moving off the plugin, %s before", after, before)
	}
	// The site is then deleted with the object cache on.
}

//go:embed testdata/objectcache-check.php
var objectCacheCheck []byte

// checkScript writes the PHP check to a file the site user can read.
func checkScript(t *testing.T) string {
	t.Helper()
	p := "/tmp/layte-objectcache-check.php"
	if err := os.WriteFile(p, objectCacheCheck, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
