package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/wpobjectcache"
)

const (
	shopWP     = "/var/www/shop/public/"
	shopDropIn = shopWP + wpobjectcache.DropInRel
)

func shopObjectCacheSite(t *testing.T, d Deps) {
	t.Helper()
	adoptedWPShop(t, d)
	d.FS.MkdirAll(shopWP+"wp-content/plugins", 0o755)
}

func calls(d Deps, from int) string {
	return strings.Join(d.Runner.(*system.FakeRunner).CallList()[from:], "\n")
}

func TestObjectCacheEnableDisable(t *testing.T) {
	d := devDeps(t)
	shopObjectCacheSite(t, d)
	p := ObjectCacheParams{WPLoginParams: shopLogin}

	if _, err := d.objectCacheEnable(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	all := calls(d, 0)
	for _, want := range []string{
		"install redis-server php8.2-redis",  // missing packages only
		"disable --now redis-server.service", // the shared default instance
		"enable --now layte-redis@shopexampleorg.service",
		"config set LAYTE_REDIS_SOCKET /run/layte-redis/shopexampleorg/redis.sock --type=constant",
		"config set LAYTE_REDIS_PREFIX shopexampleorg: --type=constant",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "plugin install") {
		t.Error("installed a third-party plugin")
	}
	for rel, want := range wpobjectcache.Files() {
		if b, _ := d.FS.ReadFile(shopWP + rel); string(b) != want {
			t.Errorf("%s not installed", rel)
		}
	}
	unit, _ := d.FS.ReadFile(redisUnitTemplate)
	for _, want := range []string{"User=%i", "--port 0", "--unixsocketperm 700", "--maxmemory 32mb", `--save ""`, "RuntimeDirectoryMode=0700"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("unit lacks %q", want)
		}
	}
	st, _ := d.objectCacheStatus(ctx, agent.Actor{}, p)
	if s := st.(ObjectCacheStatus); s.DropIn != "layte" || s.Paused || !s.Running {
		t.Errorf("status: %+v", s)
	}

	// Disabled in WordPress (Settings → Object Cache): paused; Resume flushes and clears it.
	d.FS.WriteFileAtomic(shopWP+wpobjectcache.PausedRel, []byte("paused\n"), 0o644)
	if st, _ := d.objectCacheStatus(ctx, agent.Actor{}, p); !st.(ObjectCacheStatus).Paused {
		t.Error("pause not seen")
	}
	mark := len(d.Runner.(*system.FakeRunner).CallList())
	if _, err := d.objectCacheResume(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls(d, mark), "redis.sock FLUSHDB") {
		t.Error("resume didn't flush stale data")
	}
	if ok, _ := d.FS.Exists(shopWP + wpobjectcache.PausedRel); ok {
		t.Error("still paused after resume")
	}

	// Off: Layte's files gone, Redis stopped, constants removed.
	mark = len(d.Runner.(*system.FakeRunner).CallList())
	if _, err := d.objectCacheDisable(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	for rel := range wpobjectcache.Files() {
		if ok, _ := d.FS.Exists(shopWP + rel); ok {
			t.Errorf("%s left", rel)
		}
	}
	for _, want := range []string{"disable --now layte-redis@shopexampleorg.service", "config delete LAYTE_REDIS_SOCKET"} {
		if !strings.Contains(calls(d, mark), want) {
			t.Errorf("missing %q", want)
		}
	}
}

// A site set up by an earlier version (Redis Object Cache plugin) moves to
// Layte's cache; the plugin goes, as Layte installed it.
func TestObjectCacheMovesOffPlugin(t *testing.T) {
	d := devDeps(t)
	shopObjectCacheSite(t, d)
	d.FS.MkdirAll(shopWP+"wp-content/plugins/redis-cache/includes", 0o755)
	d.FS.WriteFileAtomic(shopDropIn, []byte("<?php\n/*\nPlugin Name: Redis Object Cache Drop-In\n*/\n"), 0o644)
	if _, err := d.objectCacheEnable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin, RemovePlugin: true}); err != nil {
		t.Fatal(err)
	}
	all := calls(d, 0)
	for _, want := range []string{"plugin deactivate redis-cache", "plugin delete redis-cache", "config delete WP_REDIS_PATH"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if b, _ := d.FS.ReadFile(shopDropIn); string(b) != wpobjectcache.DropIn {
		t.Error("drop-in not replaced with Layte's")
	}
}

func TestObjectCacheRefusesForeignDropIn(t *testing.T) {
	d := devDeps(t)
	shopObjectCacheSite(t, d)
	d.FS.WriteFileAtomic(shopDropIn, []byte("<?php // another cache plugin\n"), 0o644)
	_, err := d.objectCacheEnable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin})
	if err == nil || !strings.Contains(err.Error(), "another object cache") {
		t.Fatalf("got %v", err)
	}
	if b, _ := d.FS.ReadFile(shopDropIn); !strings.Contains(string(b), "another cache plugin") {
		t.Error("foreign drop-in changed")
	}
	if strings.Contains(calls(d, 0), "apt-get") {
		t.Error("installed packages before refusing")
	}
	// Turning off never touches it either.
	d.objectCacheDisable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin})
	if ok, _ := d.FS.Exists(shopDropIn); !ok {
		t.Error("foreign drop-in deleted")
	}
}

func TestObjectCacheRollsBackWhenSiteBreaks(t *testing.T) {
	d := devDeps(t)
	shopObjectCacheSite(t, d)
	n := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		if n++; n == 1 {
			return 200, nil
		}
		return 500, nil
	}
	if _, err := d.objectCacheEnable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin}); err == nil || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("got %v", err)
	}
	for rel := range wpobjectcache.Files() {
		if ok, _ := d.FS.Exists(shopWP + rel); ok {
			t.Errorf("%s left after rollback", rel)
		}
	}
	if !strings.Contains(calls(d, 0), "disable --now layte-redis@shopexampleorg.service") {
		t.Error("Redis not stopped after rollback")
	}
}
