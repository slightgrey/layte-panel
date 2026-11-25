package actions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
	"github.com/slightgrey/layte-panel/internal/wpobjectcache"
)

// Object cache (Redis) for WordPress sites: one small Redis per site, run
// by systemd as the site's own user, on a Unix socket in a folder only that
// user can open (/run/layte-redis/<user>/redis.sock) and with no network
// port. Other sites and other local users can't connect at all; no
// passwords or ACLs are needed. Nothing is saved to disk (it's a cache) and
// each instance has a memory cap, evicting the least recently used keys.
//
// WordPress uses it through Layte's own object cache (package wpobjectcache):
// a small drop-in, the cache code and a settings page in mu-plugins, installed
// as the site's user. Layte sets the socket path and a key prefix in
// wp-config.php (LAYTE_REDIS_SOCKET, LAYTE_REDIS_PREFIX). Sites set up by
// earlier versions with the Redis Object Cache plugin move to it when the
// object cache is turned on again.
//
// Redis's own default instance (127.0.0.1:6379, no password) isn't used:
// when Layte installs Redis, it stops and disables that instance.

const (
	redisServerBin    = "/usr/bin/redis-server"
	redisCliBin       = "/usr/bin/redis-cli"
	redisUnitTemplate = systemdUnitDir + "/layte-redis@.service"
	redisRunDir       = "/run/layte-redis"
	// RedisMaxMemoryMB is each site's cap.
	RedisMaxMemoryMB = 32
	// The Redis Object Cache plugin (used by earlier versions); its drop-in
	// identifies itself with redisDropInMark.
	redisPlugin     = "redis-cache"
	redisDropInMark = "Redis Object Cache Drop-In"
)

func redisUnit(user string) string   { return "layte-redis@" + user + ".service" }
func redisSocket(user string) string { return redisRunDir + "/" + user + "/redis.sock" }
func redisExtIni(v string) string    { return "/etc/php/" + v + "/mods-available/redis.ini" }

var redisUnitContent = system.Marker + ` — do not edit by hand
# One small Redis per WordPress site (object cache), run as the site's user
# on a Unix socket only that user can open. Nothing is saved to disk.
[Unit]
Description=Layte object cache (Redis) for %i
After=network.target

[Service]
Type=notify
User=%i
Group=%i
RuntimeDirectory=layte-redis/%i
RuntimeDirectoryMode=0700
ExecStart=` + redisServerBin + ` --port 0 --unixsocket ` + redisRunDir + `/%i/redis.sock --unixsocketperm 700 --supervised systemd --daemonize no --save "" --appendonly no --maxmemory ` + strconv.Itoa(RedisMaxMemoryMB) + `mb --maxmemory-policy allkeys-lru --dir ` + redisRunDir + `/%i
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
Restart=on-failure

[Install]
WantedBy=multi-user.target
`

// ObjectCacheParams name a WordPress site created or adopted by Layte (the
// same fields as WP-Admin login and WP Debug).
type ObjectCacheParams struct {
	WPLoginParams
	// The Redis Object Cache plugin (earlier versions) was installed by Layte,
	// so it is deleted when the site moves off it.
	RemovePlugin bool `json:"remove_plugin,omitempty"`
}

// ObjectCacheResult is returned by object_cache.enable.
type ObjectCacheResult struct {
	InstalledPlugin bool     `json:"installed_plugin"` // Layte installed the plugin (vs. it was already there)
	Keys            int      `json:"keys"`             // keys in Redis after a page load
	Steps           []string `json:"steps"`
}

// objectCacheInstall installs Redis and the PHP extension for the given PHP
// version if they're missing (apt, outside the agent's sandbox like updates).
func (d Deps) objectCacheInstall(ctx context.Context, version string) ([]string, error) {
	var pkgs, steps []string
	freshRedis := false
	if ok, _ := d.FS.Exists(redisServerBin); !ok {
		pkgs = append(pkgs, "redis-server")
		freshRedis = true
	}
	if ok, _ := d.FS.Exists(redisExtIni(version)); !ok {
		pkgs = append(pkgs, "php"+version+"-redis")
	}
	if len(pkgs) == 0 {
		return nil, nil
	}
	env := []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a", "APT_LISTCHANGES_FRONTEND=none"}
	args := append([]string{"-y", "-q", "--no-install-recommends", "-o", "DPkg::Lock::Timeout=" + aptLockWaitSecs,
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "install"}, pkgs...)
	if res, err := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: aptGetBin, Args: args, Env: env})); err != nil {
		return nil, agent.Errorf("apt_failed", "apt-get install %s: %s", strings.Join(pkgs, " "), failure(res, err, 4))
	}
	steps = append(steps, "Installed "+strings.Join(pkgs, ", "))
	if freshRedis {
		// Redis's default instance (shared, no password) isn't used.
		d.Runner.Run(ctx, systemctl, "disable", "--now", "redis-server.service")
		steps = append(steps, "Stopped Redis's shared default instance (each site gets its own)")
	}
	for _, want := range []string{redisServerBin, redisExtIni(version)} {
		if ok, _ := d.FS.Exists(want); !ok {
			return nil, fmt.Errorf("%s is still missing after installing %s", want, strings.Join(pkgs, " "))
		}
	}
	return steps, nil
}

// objectCacheSite is a WordPress site the object cache works on: the
// WP-CLI site plus helpers for files relative to the WordPress folder, run
// as the site's user in its jail.
type objectCacheSite struct {
	wpSite
	d   Deps
	ctx context.Context
}

func (o objectCacheSite) rel(p string) string {
	wp := strings.TrimPrefix(strings.TrimPrefix(o.wpPath, o.root), "/")
	if wp == "" {
		return p
	}
	return wp + "/" + p
}

func (o objectCacheSite) file(req fileops.Request) (fileops.Response, error) {
	req.Root = o.root
	r, err := o.d.FileOps.Run(o.ctx, o.as, req)
	if err == nil && r.Error != "" {
		err = agent.Errorf("fs_error", "%s", r.Error)
	}
	return r, err
}

// read returns a file's content ("" and false if it doesn't exist).
func (o objectCacheSite) read(p string) (string, bool) {
	if st, err := o.file(fileops.Request{Op: "stat", Path: o.rel(p)}); err != nil || st.Entry == nil {
		return "", false
	}
	r, err := o.file(fileops.Request{Op: "read", Path: o.rel(p), Length: 1 << 20})
	if err != nil {
		return "", false
	}
	return string(r.Data), true
}

func (o objectCacheSite) remove(p string) {
	o.d.FileOps.Run(o.ctx, o.as, fileops.Request{Op: "delete", Root: o.root, Path: o.rel(p)})
}

// dropInKind says whose wp-content/object-cache.php is there.
func dropInKind(content string, exists bool) string {
	switch {
	case !exists:
		return "missing"
	case strings.Contains(content, wpobjectcache.Mark) && strings.Contains(content, "Layte Object Cache"):
		return "layte"
	case strings.Contains(content, redisDropInMark):
		return "redis-cache" // the Redis Object Cache plugin's (earlier Layte versions)
	}
	return "other"
}

// Constants in wp-config.php: Layte's, and the Redis Object Cache plugin's
// (removed when a site moves to Layte's cache).
var (
	layteRedisConsts  = []string{"LAYTE_REDIS_SOCKET", "LAYTE_REDIS_PREFIX"}
	pluginRedisConsts = []string{"WP_REDIS_SCHEME", "WP_REDIS_PATH", "WP_REDIS_PREFIX"}
)

// removeRedisPlugin takes the Redis Object Cache plugin off a site that
// moves to Layte's cache (deleted only when Layte installed it).
func (d Deps) removeRedisPlugin(ctx context.Context, s wpSite, delete bool) {
	d.wpCLI(ctx, s, "plugin", "deactivate", redisPlugin)
	if delete {
		d.wpCLI(ctx, s, "plugin", "delete", redisPlugin)
	}
	for _, c := range pluginRedisConsts {
		d.wpCLI(ctx, s, "config", "delete", c, "--type=constant")
	}
}

// objectCacheEnable gives the site its own Redis and installs Layte's
// object cache (drop-in, cache code, settings page). Also used to update or
// repair it, and to move a site off the Redis Object Cache plugin. Every
// step is undone if a later one fails or the site answers differently.
func (d Deps) objectCacheEnable(ctx context.Context, _ agent.Actor, p ObjectCacheParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p.WPLoginParams)
	if err != nil {
		return nil, err
	}
	o := objectCacheSite{wpSite: s, d: d, ctx: ctx}
	res := ObjectCacheResult{}
	step := func(m string) { res.Steps = append(res.Steps, m) }

	// Only Layte's drop-in (or the plugin's it replaces) may be there.
	cur, exists := o.read(wpobjectcache.DropInRel)
	kind := dropInKind(cur, exists)
	if kind == "other" {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s already has another object cache (wp-content/object-cache.php); remove it first", p.Domain)
	}

	steps, err := d.objectCacheInstall(ctx, s.version)
	if err != nil {
		return nil, err
	}
	res.Steps = append(res.Steps, steps...)
	before, err := d.siteStatus(ctx, s.discover)
	if err != nil {
		return nil, agent.Errorf("site_check_failed", "%s doesn't answer before the change (%v); fix that first", p.Domain, err)
	}

	var undo []func()
	fail := func(err error) (any, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}
	bg := context.WithoutCancel(ctx)
	ob := objectCacheSite{wpSite: s, d: d, ctx: bg}

	// 1. PHP-FPM must have the redis extension loaded.
	if len(steps) > 0 {
		d.Runner.RunCmd(ctx, fpmReload(s.version))
		if err := d.waitFPMSocket(ctx, s.discover.PHPSocket); err != nil {
			return fail(err)
		}
	}

	// 2. The site's own Redis, started empty.
	if err := d.FS.MkdirAll(systemdUnitDir, 0o755); err != nil {
		return fail(err)
	}
	daemonReload := system.Cmd{Bin: systemctl, Args: []string{"daemon-reload"}}
	if err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{Path: redisUnitTemplate, Content: []byte(redisUnitContent), Reload: daemonReload}); err != nil {
		return fail(err)
	}
	unit := redisUnit(p.Username)
	wasRunning := false
	if r, err := d.Runner.Run(ctx, systemctl, "is-active", unit); err == nil && strings.TrimSpace(string(r.Stdout)) == "active" {
		wasRunning = true
	}
	if r, err := d.Runner.Run(ctx, systemctl, "enable", "--now", unit); err != nil {
		return fail(fmt.Errorf("start %s: %s", unit, failure(r, err, 3)))
	}
	if !wasRunning {
		undo = append(undo, func() { d.Runner.Run(bg, systemctl, "disable", "--now", unit) })
	}
	if d.WaitSocket != nil {
		wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := d.WaitSocket(wctx, redisSocket(p.Username))
		cancel()
		if err != nil {
			return fail(fmt.Errorf("Redis for %s didn't start: %w", p.Domain, err))
		}
	}
	d.Runner.Run(ctx, redisCliBin, "-s", redisSocket(p.Username), "FLUSHDB")
	step(fmt.Sprintf("Redis for %s is running (%d MB, socket %s)", p.Username, RedisMaxMemoryMB, redisSocket(p.Username)))

	// 3. wp-config.php: where Redis is, and the key prefix.
	consts := [][2]string{{"LAYTE_REDIS_SOCKET", redisSocket(p.Username)}, {"LAYTE_REDIS_PREFIX", p.Username + ":"}}
	for _, c := range consts {
		if _, err := d.wpCLI(ctx, s, "config", "set", c[0], c[1], "--type=constant"); err != nil {
			return fail(err)
		}
	}
	undo = append(undo, func() {
		for _, c := range layteRedisConsts {
			d.wpCLI(bg, s, "config", "delete", c, "--type=constant")
		}
	})

	// 4. Layte's files (the drop-in last, so WordPress only loads complete code).
	if kind == "redis-cache" {
		d.removeRedisPlugin(ctx, s, p.RemovePlugin)
		step("Replaced the Redis Object Cache plugin with Layte's object cache")
	}
	o.file(fileops.Request{Op: "mkdir", Path: o.rel("wp-content/mu-plugins")})
	o.file(fileops.Request{Op: "mkdir", Path: o.rel(wpobjectcache.CacheDirRel)})
	o.remove(wpobjectcache.PausedRel) // a fresh start is never paused
	for _, rel := range []string{wpobjectcache.CacheRel, wpobjectcache.PluginRel, wpobjectcache.DropInRel} {
		if _, err := o.file(fileops.Request{Op: "write", Path: o.rel(rel), Chunk: []byte(wpobjectcache.Files()[rel]), Final: true}); err != nil {
			return fail(err)
		}
	}
	undo = append(undo, func() {
		ob.remove(wpobjectcache.DropInRel)
		ob.remove(wpobjectcache.PluginRel)
		ob.remove(wpobjectcache.CacheRel)
		ob.remove(wpobjectcache.CacheDirRel)
	})
	step("Installed Layte's object cache (wp-content/object-cache.php, mu-plugins/layte-object-cache)")

	// 5. The site must answer as before, and WordPress must use Redis.
	d.Runner.RunCmd(ctx, fpmReload(s.version)) // opcache: the drop-in is new
	after, err := d.settledStatus(ctx, s.discover, before)
	if err != nil || after != before {
		return fail(agent.Errorf("site_check_failed", "%s answered %d before and %d after; everything was undone", p.Domain, before, after))
	}
	res.Keys = d.redisKeys(ctx, p.Username)
	if res.Keys == 0 && d.WaitSocket != nil {
		return fail(agent.Errorf("site_check_failed", "WordPress didn't store anything in Redis; everything was undone"))
	}
	step(fmt.Sprintf("%s still answers %d; %d keys cached", p.Domain, after, res.Keys))
	return res, nil
}

// redisKeys counts the keys in the site's Redis (0 if it can't tell).
func (d Deps) redisKeys(ctx context.Context, user string) int {
	r, err := d.Runner.Run(ctx, redisCliBin, "-s", redisSocket(user), "DBSIZE")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(r.Stdout)))
	return n
}

// objectCacheDisable removes the object cache and stops the site's Redis
// (idempotent). Files Layte didn't write are left alone; the Redis Object
// Cache plugin (earlier Layte versions) is deleted only if Layte installed it.
func (d Deps) objectCacheDisable(ctx context.Context, _ agent.Actor, p ObjectCacheParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p.WPLoginParams)
	if err != nil {
		return nil, err
	}
	o := objectCacheSite{wpSite: s, d: d, ctx: ctx}
	cur, exists := o.read(wpobjectcache.DropInRel)
	switch dropInKind(cur, exists) {
	case "layte":
		o.remove(wpobjectcache.DropInRel)
	case "redis-cache":
		o.remove(wpobjectcache.DropInRel)
		d.removeRedisPlugin(ctx, s, p.RemovePlugin)
	}
	if c, ok := o.read(wpobjectcache.PluginRel); ok && strings.Contains(c, wpobjectcache.Mark) {
		o.remove(wpobjectcache.PluginRel)
	}
	o.remove(wpobjectcache.PausedRel)
	if c, ok := o.read(wpobjectcache.CacheRel); ok && strings.Contains(c, wpobjectcache.Mark) {
		o.remove(wpobjectcache.CacheRel)
		o.remove(wpobjectcache.CacheDirRel)
	}
	for _, c := range layteRedisConsts {
		d.wpCLI(ctx, s, "config", "delete", c, "--type=constant")
	}
	d.Runner.RunCmd(ctx, fpmReload(s.version))
	if r, err := d.Runner.Run(ctx, systemctl, "disable", "--now", redisUnit(p.Username)); err != nil {
		return nil, fmt.Errorf("stop %s: %s", redisUnit(p.Username), failure(r, err, 3))
	}
	return map[string]bool{"disabled": true}, nil
}

// objectCacheFlush empties the site's Redis (its own instance only).
func (d Deps) objectCacheFlush(ctx context.Context, _ agent.Actor, p ObjectCacheParams) (any, error) {
	if _, err := d.wpSiteFor(ctx, p.WPLoginParams); err != nil {
		return nil, err
	}
	if r, err := d.Runner.Run(ctx, redisCliBin, "-s", redisSocket(p.Username), "FLUSHDB"); err != nil {
		return nil, agent.Errorf("redis_failed", "flush: %s", failure(r, err, 2))
	}
	return map[string]bool{"flushed": true}, nil
}

// objectCacheResume turns the cache back on after it was disabled in
// WordPress: Redis is emptied first (its data may be stale), then the pause
// flag goes.
func (d Deps) objectCacheResume(ctx context.Context, _ agent.Actor, p ObjectCacheParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p.WPLoginParams)
	if err != nil {
		return nil, err
	}
	if r, err := d.Runner.Run(ctx, redisCliBin, "-s", redisSocket(p.Username), "FLUSHDB"); err != nil {
		return nil, agent.Errorf("redis_failed", "Redis for %s isn't running (%s); turn the object cache off and on again", p.Domain, failure(r, err, 2))
	}
	objectCacheSite{wpSite: s, d: d, ctx: ctx}.remove(wpobjectcache.PausedRel)
	return map[string]bool{"resumed": true}, nil
}

// ObjectCacheStatus is the site's object cache right now.
type ObjectCacheStatus struct {
	Running    bool   `json:"running"`
	Paused     bool   `json:"paused"`  // disabled in WordPress (Settings → Object Cache)
	DropIn     string `json:"drop_in"` // layte | redis-cache | missing | other
	Keys       int    `json:"keys"`
	UsedBytes  int64  `json:"used_bytes"`
	LimitBytes int64  `json:"limit_bytes"`
}

// objectCacheStatus reads the site's Redis and files (read-only).
func (d Deps) objectCacheStatus(ctx context.Context, _ agent.Actor, p ObjectCacheParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p.WPLoginParams)
	if err != nil {
		return nil, err
	}
	o := objectCacheSite{wpSite: s, d: d, ctx: ctx}
	st := ObjectCacheStatus{LimitBytes: RedisMaxMemoryMB << 20}
	cur, exists := o.read(wpobjectcache.DropInRel)
	st.DropIn = dropInKind(cur, exists)
	_, st.Paused = o.read(wpobjectcache.PausedRel)
	r, err := d.Runner.Run(ctx, redisCliBin, "-s", redisSocket(p.Username), "INFO", "memory")
	if err != nil {
		return st, nil // not running
	}
	st.Running = true
	for _, line := range strings.Split(string(r.Stdout), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "used_memory:"); ok {
			st.UsedBytes, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	st.Keys = d.redisKeys(ctx, p.Username)
	return st, nil
}

// ObjectCachePrepareParams name a PHP version about to be used by a site
// with the object cache on (e.g. before a PHP switch).
type ObjectCachePrepareParams struct {
	PHPVersion string `json:"php_version"`
}

func (p ObjectCachePrepareParams) Validate() error {
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion})
}

// objectCachePrepare installs Redis's PHP extension for a PHP version and
// reloads that PHP-FPM, so a site can switch to it with its object cache.
func (d Deps) objectCachePrepare(ctx context.Context, _ agent.Actor, p ObjectCachePrepareParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	steps, err := d.objectCacheInstall(ctx, p.PHPVersion)
	if err != nil {
		return nil, err
	}
	if len(steps) > 0 {
		d.Runner.RunCmd(ctx, fpmReload(p.PHPVersion))
	}
	return map[string][]string{"steps": steps}, nil
}
