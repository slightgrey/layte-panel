package actions

import (
	"context"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Page cache (nginx FastCGI cache) for WordPress sites Layte created. The
// cache itself is configured in the site's vhost (nginx.vhost_write with
// SiteConfigParams.Cache); these actions manage its files and the
// must-use plugin that asks for a purge when content changes.

// CacheParams name a Layte site.
type CacheParams struct {
	Domain   string `json:"domain"`
	Username string `json:"username"`
	// cache.purge: also remove the folder itself (the cache was turned off).
	Remove bool `json:"remove,omitempty"`
	// cache.plugin: install (true) or remove (false) the must-use plugin.
	Install bool `json:"install,omitempty"`
}

func (p CacheParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	return validate.SiteUsername(p.Username)
}

// cachePurge empties the site's cache folder (or removes it). nginx copes
// with cache files disappearing; it just fetches the pages again.
func (d Deps) cachePurge(_ context.Context, _ agent.Actor, p CacheParams) (any, error) {
	n, err := d.purgeCacheDir(p.Domain, p.Remove)
	if err != nil {
		return nil, err
	}
	return map[string]int{"removed": n}, nil
}

func (d Deps) purgeCacheDir(domain string, removeDir bool) (int, error) {
	dir := pagecache.Dir(domain)
	fi, err := d.FS.Lstat(dir)
	if err != nil {
		return 0, nil // nothing cached yet
	}
	if !fi.IsDir() {
		return 0, agent.Errorf(agent.CodeInvalidParams, "%s is not a folder", dir)
	}
	if removeDir {
		return 1, d.FS.RemoveAll(dir)
	}
	entries, _ := d.FS.Glob(dir + "/*")
	for _, e := range entries {
		if err := d.FS.RemoveAll(e); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

// cachePlugin installs or removes the must-use plugin, as the site user. A
// file of the same name that Layte didn't write is never touched.
func (d Deps) cachePlugin(ctx context.Context, _ agent.Actor, p CacheParams) (any, error) {
	root, as, _, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username})
	if err != nil {
		return nil, err
	}
	rel := "public/" + pagecache.PluginRel
	file := func(req fileops.Request) (fileops.Response, error) {
		req.Root = root
		res, err := d.FileOps.Run(ctx, as, req)
		if err == nil && res.Error != "" {
			err = agent.Errorf("fs_error", "%s", res.Error)
		}
		return res, err
	}
	cur, readErr := file(fileops.Request{Op: "read", Path: rel, Length: 64 << 10})
	ours := readErr == nil && strings.Contains(string(cur.Data), "Managed by Layte Panel")
	if readErr == nil && !ours {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s exists and wasn't written by Layte Panel", rel)
	}
	if !p.Install {
		if ours {
			if _, err := file(fileops.Request{Op: "delete", Path: rel}); err != nil {
				return nil, err
			}
		}
		return map[string]bool{"installed": false}, nil
	}
	if ours && string(cur.Data) == pagecache.Plugin {
		return map[string]bool{"installed": true}, nil
	}
	if st, err := file(fileops.Request{Op: "stat", Path: "public/wp-content"}); err != nil || st.Entry == nil || st.Entry.Type != "dir" {
		return nil, agent.Errorf(agent.CodeNotFound, "%s has no wp-content folder; is WordPress installed?", p.Domain)
	}
	file(fileops.Request{Op: "mkdir", Path: path.Dir(rel)}) // may already exist
	if _, err := file(fileops.Request{Op: "write", Path: rel, Chunk: []byte(pagecache.Plugin), Final: true}); err != nil {
		return nil, err
	}
	return map[string]bool{"installed": true}, nil
}

// WatchCachePurges purges a site's page cache soon after its must-use plugin
// asks for it (a marker file in <site>/tmp/). It runs in the agent until ctx
// ends. Only sites that have a cache folder are checked.
func (d Deps) WatchCachePurges(ctx context.Context, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.purgeMarked(log)
		}
	}
}

// purgeMarked handles the marker files once; it returns the purged domains.
func (d Deps) purgeMarked(log *slog.Logger) []string {
	var purged []string
	dirs, _ := d.FS.Glob(pagecache.Root + "/*")
	for _, dir := range dirs {
		domain := path.Base(dir)
		if validDomain(domain) != nil {
			continue
		}
		markers, _ := d.FS.Glob("/home/*/" + domain + "/tmp/" + pagecache.MarkerName)
		asked := false
		for _, m := range markers {
			home := path.Dir(path.Dir(path.Dir(m))) // /home/<user>
			if ok, _ := d.FS.Exists(home + "/" + siteUserMarker); !ok {
				continue // not a Layte site user
			}
			if fi, err := d.FS.Lstat(m); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			d.FS.Remove(m) // first, so a change made while purging asks again
			asked = true
		}
		if !asked {
			continue
		}
		if n, err := d.purgeCacheDir(domain, false); err != nil {
			log.Warn("page cache purge failed", "domain", domain, "err", err)
		} else {
			log.Info("page cache purged (content changed)", "domain", domain, "entries", n)
			purged = append(purged, domain)
		}
	}
	return purged
}
