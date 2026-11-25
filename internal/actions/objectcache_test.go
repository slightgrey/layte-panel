package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

const shopDropIn = "/var/www/shop/public/wp-content/object-cache.php"

func TestObjectCacheEnableDisable(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	d.FS.MkdirAll("/var/www/shop/public/wp-content/plugins", 0o755)
	p := ObjectCacheParams{WPLoginParams: shopLogin}

	out, err := d.objectCacheEnable(ctx, agent.Actor{}, p)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(ObjectCacheResult)
	if !r.InstalledPlugin {
		t.Errorf("result: %+v", r)
	}
	calls := strings.Join(d.Runner.(*system.FakeRunner).CallList(), "\n")
	for _, want := range []string{
		"install redis-server php8.2-redis",  // missing packages only
		"disable --now redis-server.service", // the shared default instance
		"enable --now layte-redis@shopexampleorg.service",
		"config set WP_REDIS_SCHEME unix --type=constant",
		"config set WP_REDIS_PATH /run/layte-redis/shopexampleorg/redis.sock --type=constant",
		"config set WP_REDIS_PREFIX shopexampleorg: --type=constant",
		"plugin install redis-cache",
		"plugin activate redis-cache",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in:\n%s", want, calls)
		}
	}
	unit, _ := d.FS.ReadFile(redisUnitTemplate)
	for _, want := range []string{"User=%i", "--port 0", "--unixsocketperm 700", "--maxmemory 32mb", `--save ""`, "RuntimeDirectoryMode=0700"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if b, _ := d.FS.ReadFile(shopDropIn); !strings.Contains(string(b), redisDropInMark) {
		t.Errorf("drop-in: %q", b)
	}

	// Again: nothing to install, the plugin is kept.
	mark := len(d.Runner.(*system.FakeRunner).CallList())
	if out, err := d.objectCacheEnable(ctx, agent.Actor{}, p); err != nil || out.(ObjectCacheResult).InstalledPlugin {
		t.Errorf("second enable: %+v %v", out, err)
	}
	if calls := strings.Join(d.Runner.(*system.FakeRunner).CallList()[mark:], "\n"); strings.Contains(calls, "apt-get") || strings.Contains(calls, "plugin install") {
		t.Errorf("reinstalled:\n%s", calls)
	}

	// Off: drop-in gone, Redis stopped, constants removed, plugin deleted (Layte's).
	mark = len(d.Runner.(*system.FakeRunner).CallList())
	if _, err := d.objectCacheDisable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin, RemovePlugin: true}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(shopDropIn); ok {
		t.Error("drop-in left")
	}
	calls = strings.Join(d.Runner.(*system.FakeRunner).CallList(), "\n")
	for _, want := range []string{"disable --now layte-redis@shopexampleorg.service", "plugin delete redis-cache", "config delete WP_REDIS_PATH"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in:\n%s", want, calls)
		}
	}
}

func TestObjectCacheRefusesForeignDropIn(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	d.FS.MkdirAll("/var/www/shop/public/wp-content", 0o755)
	d.FS.WriteFileAtomic(shopDropIn, []byte("<?php // another cache plugin\n"), 0o644)
	_, err := d.objectCacheEnable(ctx, agent.Actor{}, ObjectCacheParams{WPLoginParams: shopLogin})
	if err == nil || !strings.Contains(err.Error(), "another object cache") {
		t.Fatalf("got %v", err)
	}
	if b, _ := d.FS.ReadFile(shopDropIn); !strings.Contains(string(b), "another cache plugin") {
		t.Error("foreign drop-in changed")
	}
	// Undone: the site's Redis was stopped again.
	if calls := strings.Join(d.Runner.(*system.FakeRunner).CallList(), "\n"); !strings.Contains(calls, "disable --now layte-redis@shopexampleorg.service") {
		t.Errorf("Redis not stopped after refusing:\n%s", calls)
	}
}

func TestObjectCacheRollsBackWhenSiteBreaks(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	d.FS.MkdirAll("/var/www/shop/public/wp-content/plugins", 0o755)
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
	if ok, _ := d.FS.Exists(shopDropIn); ok {
		t.Error("drop-in left after rollback")
	}
	if ok, _ := d.FS.Exists("/var/www/shop/public/wp-content/plugins/redis-cache"); ok {
		t.Error("plugin Layte installed left after rollback")
	}
}
