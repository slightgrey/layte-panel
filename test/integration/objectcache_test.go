//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

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
	if b, err := os.ReadFile(dropIn); err != nil || !strings.Contains(string(b), "Redis Object Cache") {
		t.Errorf("drop-in: %v", err)
	}
	// WordPress stored its objects in the site's own Redis, under its prefix.
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

	// On again (the site is then deleted with it on).
	site, _ = e.st.SiteByID(ctx, site.ID)
	jobID, err = e.svc.SetObjectCache(ctx, e.actor, site, true)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
}
