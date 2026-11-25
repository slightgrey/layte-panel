// Package wpobjectcache holds Layte's WordPress object cache (Redis), which
// Layte installs into a site's wp-content instead of a third-party plugin:
//
//   - object-cache.php: the drop-in WordPress loads before any plugin; it
//     only loads the cache code below (or nothing, so WordPress falls back
//     to its own cache).
//   - mu-plugins/layte-object-cache/cache.php: the cache itself.
//   - mu-plugins/layte-object-cache.php: Settings → Object Cache (flush,
//     disable/enable).
//
// The files are embedded in the binary and installed as the site's user.
package wpobjectcache

import _ "embed"

// Mark is in every file Layte installs, so it never touches files it didn't write.
const Mark = "Managed by Layte Panel"

// Where the files go, relative to the WordPress folder.
const (
	DropInRel   = "wp-content/object-cache.php"
	CacheDirRel = "wp-content/mu-plugins/layte-object-cache"
	CacheRel    = CacheDirRel + "/cache.php"
	PluginRel   = "wp-content/mu-plugins/layte-object-cache.php"
	// PausedRel is the flag the settings page creates to disable the cache.
	PausedRel = CacheDirRel + "/.paused"
)

var (
	//go:embed object-cache.php
	DropIn string
	//go:embed cache.php
	Cache string
	//go:embed layte-object-cache.php
	Plugin string
)

// Files maps each file's path (relative to the WordPress folder) to its content.
func Files() map[string]string {
	return map[string]string{DropInRel: DropIn, CacheRel: Cache, PluginRel: Plugin}
}
