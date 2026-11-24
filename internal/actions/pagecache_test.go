package actions

import (
	"io"
	"log/slog"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/pagecache"
)

// cacheSite makes a Layte site user acme with a.test and a WordPress folder.
func cacheSite(t *testing.T, d Deps) {
	t.Helper()
	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.siteDirsCreate(ctx, agent.Actor{}, DirsParams{Username: "acme", Domain: "a.test"}); err != nil {
		t.Fatal(err)
	}
	d.FS.MkdirAll("/home/acme/a.test/public/wp-content", 0o755)
}

func TestCachePlugin(t *testing.T) {
	d := devDeps(t)
	cacheSite(t, d)
	plugin := "/home/acme/a.test/public/" + pagecache.PluginRel
	p := CacheParams{Domain: "a.test", Username: "acme", Install: true}

	if _, err := d.cachePlugin(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.FS.ReadFile(plugin); string(b) != pagecache.Plugin {
		t.Errorf("plugin not installed: %q", b)
	}
	p.Install = false
	if _, err := d.cachePlugin(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(plugin); ok {
		t.Error("plugin not removed")
	}

	// A file of the same name that Layte didn't write stays.
	d.FS.WriteFileAtomic(plugin, []byte("<?php // mine\n"), 0o644)
	if _, err := d.cachePlugin(ctx, agent.Actor{}, CacheParams{Domain: "a.test", Username: "acme", Install: true}); err == nil {
		t.Error("replaced a foreign file")
	}
	if _, err := d.cachePlugin(ctx, agent.Actor{}, CacheParams{Domain: "a.test", Username: "acme"}); err == nil {
		t.Error("removed a foreign file")
	}
	if b, _ := d.FS.ReadFile(plugin); string(b) != "<?php // mine\n" {
		t.Error("foreign file changed")
	}
}

func TestCachePurgeAndMarkers(t *testing.T) {
	d := devDeps(t)
	cacheSite(t, d)
	dir := pagecache.Dir("a.test")
	d.FS.MkdirAll(dir+"/a/bc", 0o700)
	d.FS.WriteFileAtomic(dir+"/a/bc/page", []byte("cached"), 0o600)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// No marker: nothing happens.
	if got := d.purgeMarked(log); len(got) != 0 {
		t.Errorf("purged without a marker: %v", got)
	}
	// The plugin's marker: purged, marker gone, folder kept.
	marker := "/home/acme/a.test/tmp/" + pagecache.MarkerName
	d.FS.WriteFileAtomic(marker, nil, 0o644)
	if got := d.purgeMarked(log); len(got) != 1 || got[0] != "a.test" {
		t.Errorf("purged %v", got)
	}
	if ok, _ := d.FS.Exists(marker); ok {
		t.Error("marker kept")
	}
	if ok, _ := d.FS.Exists(dir + "/a/bc/page"); ok {
		t.Error("cache not purged")
	}
	if ok, _ := d.FS.Exists(dir); !ok {
		t.Error("cache folder removed")
	}

	// A marker in a home that isn't a Layte site user is ignored.
	d.FS.MkdirAll("/home/eve/a.test/tmp", 0o755)
	d.FS.WriteFileAtomic("/home/eve/a.test/tmp/"+pagecache.MarkerName, nil, 0o644)
	if got := d.purgeMarked(log); len(got) != 0 {
		t.Errorf("purged for a foreign marker: %v", got)
	}

	// Turning the cache off removes the folder.
	if _, err := d.cachePurge(ctx, agent.Actor{}, CacheParams{Domain: "a.test", Username: "acme", Remove: true}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(dir); ok {
		t.Error("cache folder not removed")
	}
}
