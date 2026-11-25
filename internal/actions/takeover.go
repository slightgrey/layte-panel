package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/nginx"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
)

// Taking over an adopted site's nginx config: the admin's vhost is replaced
// by one rendered from Layte's template (the files stay in their web root),
// and the adopted pool by a standard Layte pool. From then on PHP switching,
// PHP settings and the page cache work as for sites Layte created.
//
// The admin's vhost is kept: a regular file is moved to <name>.layte-orig
// (sites-available for files that were in sites-enabled), a sites-enabled
// symlink is just removed (its target stays). The adopted pool's original
// from before the adoption (<pool>.layte-orig) stays as well.

// TakeoverParams name an adopted site. SSL: the new vhost serves HTTPS,
// with the certificate the admin's vhost already uses (OwnCert, its certbot
// live folder) or else the site's Layte certificate (issued beforehand).
// VhostHash comes from the preview the admin confirmed.
type TakeoverParams struct {
	Domain    string `json:"domain"`
	Username  string `json:"username"`
	WebRoot   string `json:"web_root"`
	SSL       bool   `json:"ssl,omitempty"`
	OwnCert   string `json:"own_cert,omitempty"`
	VhostHash string `json:"vhost_hash,omitempty"`
}

func (p TakeoverParams) Validate() error {
	if p.OwnCert != "" {
		if err := validOwnCert(p.OwnCert); err != nil {
			return err
		}
	}
	return FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}.validate(readOps)
}

// OwnCertOf is the certbot live folder of the site's current certificate,
// or "" if it doesn't use one.
func OwnCertOf(certPath string) string {
	dir := path.Dir(certPath)
	if path.Base(certPath) != "fullchain.pem" || validOwnCert(dir) != nil {
		return ""
	}
	return dir
}

// TakeoverPreview is what taking over would change. Nothing is changed.
type TakeoverPreview struct {
	Domain       string   `json:"domain"`
	WebRoot      string   `json:"web_root"`
	PHPVersion   string   `json:"php_version"`
	SSL          bool     `json:"ssl"`
	OwnCert      string   `json:"own_cert"`   // the certificate kept for now ("" = none usable)
	VhostFile    string   `json:"vhost_file"` // the admin's file, as nginx loads it
	KeptAs       string   `json:"kept_as"`    // where it goes ("" = only the link is removed)
	NewVhostFile string   `json:"new_vhost_file"`
	PoolFile     string   `json:"pool_file"`
	NewPoolFile  string   `json:"new_pool_file"`
	VhostDiff    []string `json:"vhost_diff"` // "+ line" / "- line" / "  line"
	PoolDiff     []string `json:"pool_diff"`
	VhostHash    string   `json:"vhost_hash"`
	Blockers     []string `json:"blockers,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// takeover is everything the preview and the takeover need.
type takeover struct {
	site     DiscoveredSite
	pool     poolFile
	enabled  string // the admin's vhost as nginx loads it
	keep     string
	oldVhost []byte
	newVhost templates.Site
	vhost    string // new vhost path
	link     string // its enable link ("" for conf.d)
	blockers []string
	warnings []string
}

func vhostHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// takeoverTarget checks, from the server's own config, that domain + user +
// web root is an adopted site whose vhost is still the admin's.
func (d Deps) takeoverTarget(ctx context.Context, p TakeoverParams) (takeover, error) {
	var t takeover
	if err := d.verifyAdopted(ctx, p.Domain, p.Username, p.WebRoot); err != nil {
		return t, err
	}
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return t, err
	}
	all := out.([]DiscoveredSite)
	i := slices.IndexFunc(all, func(s DiscoveredSite) bool { return s.Domain == p.Domain && s.Root == p.WebRoot })
	if i < 0 {
		return t, agent.Errorf(agent.CodeNotFound, "%s is not served from %s", p.Domain, p.WebRoot)
	}
	t.site = all[i]
	if t.site.Managed {
		return t, agent.Errorf(agent.CodeInvalidParams, "Layte Panel already manages the nginx config of %s", p.Domain)
	}
	pools := d.findPools(t.site.PHPSocket)
	if len(pools) != 1 {
		return t, agent.Errorf(agent.CodeInvalidParams, "%s's PHP pool isn't an adopted pool", p.Domain)
	}
	t.pool = pools[0]

	// The file must hold this site only: everything in it stops being loaded.
	if t.oldVhost, err = d.FS.ReadFile(t.site.ConfigFile); err != nil {
		return t, fmt.Errorf("read %s: %w", t.site.ConfigFile, err)
	}
	ds, err := nginx.Parse(string(t.oldVhost))
	if err != nil {
		return t, agent.Errorf(agent.CodeInvalidParams, "can't read %s: %v", t.site.ConfigFile, err)
	}
	names := append([]string{t.site.Domain}, t.site.Aliases...)
	for _, srv := range servers(ds) {
		s := d.siteFromServer(srv.Block, nil)
		switch {
		case s == nil:
			t.blockers = append(t.blockers, t.site.ConfigFile+" also has a catch-all (default) server block")
		case !slices.Contains(names, s.Domain):
			t.blockers = append(t.blockers, fmt.Sprintf("%s also serves %s; give that site its own file first", t.site.ConfigFile, s.Domain))
		}
	}
	if len(ds) != len(servers(ds)) || slices.ContainsFunc(ds, func(x *nginx.Directive) bool { return x.Name == "http" }) {
		t.blockers = append(t.blockers, t.site.ConfigFile+" has directives outside its server blocks")
	}

	// Where nginx loads it from: exactly one enabled entry.
	var enabled []string
	for _, pattern := range []string{nginxDir + "/sites-enabled/*", nginxDir + "/conf.d/*.conf"} {
		m, _ := d.FS.Glob(pattern)
		for _, f := range m {
			if real, err := d.FS.Resolve(f); err == nil && real == t.site.ConfigFile {
				enabled = append(enabled, f)
			}
		}
	}
	if len(enabled) != 1 {
		t.blockers = append(t.blockers, fmt.Sprintf("%s is enabled %d times; expected once", t.site.ConfigFile, len(enabled)))
	} else {
		t.enabled = enabled[0]
		if _, isLink, err := d.FS.LinkTarget(t.enabled); err == nil && !isLink {
			if path.Dir(t.enabled) == nginxDir+"/conf.d" {
				t.keep = t.enabled + system.AdoptSuffix
			} else {
				t.keep = nginxDir + "/sites-available/" + path.Base(t.enabled) + system.AdoptSuffix
			}
			if ok, _ := d.FS.Exists(t.keep); ok {
				t.blockers = append(t.blockers, t.keep+" already exists; move it away first")
			}
		}
	}

	t.vhost, t.link = d.vhostFiles(p.Domain)
	for _, f := range []string{t.vhost, t.link} {
		if ok, _ := d.FS.Exists(f); f != "" && ok {
			t.blockers = append(t.blockers, f+" already exists; move it away first")
		}
	}
	if newPool := templates.PoolPath(t.pool.Version, p.Domain); newPool == t.pool.Path {
		t.blockers = append(t.blockers, t.pool.Path+" already has the name of a Layte pool")
	}
	t.newVhost = templates.Site{Domain: p.Domain, Aliases: t.site.Aliases, User: p.Username, PHPVersion: t.pool.Version,
		SSL: t.site.SSL, WebRoot: p.WebRoot, WebUser: d.webUser(), OwnCert: OwnCertOf(t.site.CertPath)}
	t.warnings = customDirectives(ds)
	return t, nil
}

// templateLocations are the location blocks Layte's vhost has itself.
var templateLocations = []string{"/", `~ \.php$`, "= /wp-config.php", "^~ /.well-known/acme-challenge/"}

// customDirectives lists what the admin's vhost does that Layte's template
// won't (Certbot's redirects and certificate lines are replaced, not lost).
func customDirectives(ds []*nginx.Directive) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, srv := range servers(ds) {
		nginx.Walk(srv.Block, func(x *nginx.Directive) {
			switch x.Name {
			case "location":
				if a := strings.Join(x.Args, " "); !slices.Contains(templateLocations, a) && !strings.HasPrefix(a, "~ /\\.") &&
					!strings.HasPrefix(a, "~* \\.(") && !strings.HasPrefix(a, "~* /(?:uploads") {
					add("location " + a)
				}
			case "rewrite", "add_header", "auth_basic", "proxy_pass", "limit_req":
				add(x.Name + " " + strings.Join(x.Args, " "))
			case "include":
				if a := x.Arg(0); a != "fastcgi_params" && a != "snippets/fastcgi-php.conf" && !strings.HasPrefix(a, "/etc/letsencrypt/") {
					add("include " + a)
				}
			}
		})
	}
	return out
}

func (d Deps) takeoverPreview(ctx context.Context, _ agent.Actor, p TakeoverParams) (any, error) {
	t, err := d.takeoverTarget(ctx, p)
	if err != nil {
		return nil, err
	}
	newVhost, err := templates.Vhost(t.newVhost)
	if err != nil {
		return nil, err
	}
	newPool, err := templates.Pool(t.newVhost)
	if err != nil {
		return nil, err
	}
	return TakeoverPreview{
		Domain: p.Domain, WebRoot: p.WebRoot, PHPVersion: t.pool.Version, SSL: t.site.SSL, OwnCert: t.newVhost.OwnCert,
		VhostFile: t.enabled, KeptAs: t.keep, NewVhostFile: t.vhost,
		PoolFile: t.pool.Path, NewPoolFile: templates.PoolPath(t.pool.Version, p.Domain),
		VhostDiff: lineDiff(string(t.oldVhost), string(newVhost)), PoolDiff: lineDiff(t.pool.Content, string(newPool)),
		VhostHash: vhostHash(t.oldVhost), Blockers: t.blockers, Warnings: t.warnings,
	}, nil
}

// TakeoverResult is returned by site.nginx_takeover.
type TakeoverResult struct {
	VhostFile  string   `json:"vhost_file"`
	PoolFile   string   `json:"pool_file"`
	PHPVersion string   `json:"php_version"`
	KeptAs     string   `json:"kept_as"`
	Steps      []string `json:"steps"`
}

// siteNginxTakeover does the takeover, undoing every finished step if a
// later one fails or the site answers differently afterwards.
func (d Deps) siteNginxTakeover(ctx context.Context, _ agent.Actor, p TakeoverParams) (any, error) {
	t, err := d.takeoverTarget(ctx, p)
	if err != nil {
		return nil, err
	}
	if len(t.blockers) > 0 {
		return nil, agent.Errorf(agent.CodeInvalidParams, "can't take over %s: %s", p.Domain, strings.Join(t.blockers, "; "))
	}
	if vhostHash(t.oldVhost) != p.VhostHash {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s changed since the preview; review it again", t.site.ConfigFile)
	}
	t.newVhost.SSL = p.SSL
	if p.OwnCert != "" && p.OwnCert != t.newVhost.OwnCert {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s doesn't use the certificate in %s", p.Domain, p.OwnCert)
	}
	t.newVhost.OwnCert = p.OwnCert // "" = the Layte certificate
	if p.SSL {
		if err := d.requireCertFiles(p.Domain, t.newVhost.CertLive()); err != nil {
			return nil, err
		}
	}
	vhost, err := templates.Vhost(t.newVhost)
	if err != nil {
		return nil, err
	}
	pool, err := templates.Pool(t.newVhost)
	if err != nil {
		return nil, err
	}
	before, err := d.siteStatus(ctx, t.site)
	if err != nil {
		return nil, agent.Errorf("site_check_failed", "%s doesn't answer before the takeover (%v); fix that first", p.Domain, err)
	}

	v := t.pool.Version
	newPool := templates.PoolPath(v, p.Domain)
	res := TakeoverResult{VhostFile: t.vhost, PoolFile: newPool, PHPVersion: v, KeptAs: t.keep}
	var undo []func()
	fail := func(err error) (any, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}
	step := func(s string) { res.Steps = append(res.Steps, s) }
	bg := context.WithoutCancel(ctx)

	// 1. A standard Layte pool next to the adopted one (its own socket).
	err = system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{Path: newPool, Content: pool, Test: fpmTest(v), Reload: fpmReload(v)})
	if err != nil {
		return fail(configError(err))
	}
	undo = append(undo, func() {
		system.RemoveConfig(bg, d.FS, d.Runner, system.RemoveSpec{Path: newPool, Test: fpmTest(v), Reload: fpmReload(v)})
	})
	if err := d.waitFPMSocket(ctx, t.newVhost.Socket()); err != nil {
		return fail(err)
	}
	step(fmt.Sprintf("Started the Layte PHP %s pool %s", v, newPool))

	// 2. The admin's vhost out, Layte's in (one nginx -t and reload).
	workers := d.nginxWorkers()
	restore, err := system.ReplaceConfig(ctx, d.FS, d.Runner, system.ReplaceSpec{
		New: system.WriteSpec{Path: t.vhost, Content: vhost, Link: t.link, Test: nginxTest, Reload: nginxReload},
		Old: t.enabled, Keep: t.keep, Expected: t.oldVhost,
	})
	if err != nil {
		return fail(configError(err))
	}
	undo = append(undo, func() {
		w := d.nginxWorkers()
		restore(bg)
		d.waitNginxReloaded(bg, w)
	})
	if !d.waitNginxReloaded(ctx, workers) {
		return fail(agent.Errorf("site_check_failed", "nginx didn't finish reloading within 15 seconds; everything was undone"))
	}
	if t.keep != "" {
		step(fmt.Sprintf("Moved your nginx config %s to %s", t.enabled, t.keep))
	} else {
		step("Disabled your nginx config " + t.enabled + " (the link only)")
	}
	step("nginx now uses " + t.vhost)

	// 3. The site must answer exactly as before.
	after, err := d.siteStatus(ctx, t.site)
	if err != nil {
		return fail(agent.Errorf("site_check_failed", "%s answered %d before, and no longer answers after (%v); everything was undone", p.Domain, before, err))
	}
	if after != before {
		return fail(agent.Errorf("site_check_failed", "%s answered %d before and %d after; everything was undone", p.Domain, before, after))
	}
	step(fmt.Sprintf("%s still answers %d", p.Domain, after))

	// 4. The adopted pool is unused now; removing it is best effort. Old nginx
	// workers may still be running requests on it: give them time to finish
	// (other long-lived connections, e.g. live streams, can keep a worker
	// for much longer, so don't wait forever).
	if !d.waitNginxDrained(ctx, workers, 30*time.Second) {
		step("Old nginx workers still had open connections after 30 seconds; removing the old pool anyway")
	}
	err = system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: t.pool.Path, Test: fpmTest(v), Reload: fpmReload(v)})
	if err != nil {
		step(fmt.Sprintf("Warning: couldn't remove the old pool %s (%v); it is unused", t.pool.Path, err))
	} else {
		step("Removed the old pool " + t.pool.Path)
	}
	return res, nil
}

// verifyTakenOver checks that an adopted site's vhost is Layte's (taken
// over), at the path Layte writes it to. Config writes for adopted sites go
// through it, so Layte never adds a second vhost next to the admin's.
func (d Deps) verifyTakenOver(ctx context.Context, domain, user, webRoot string) error {
	if err := d.verifyAdopted(ctx, domain, user, webRoot); err != nil {
		return err
	}
	vhost, _ := d.vhostFiles(domain)
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(out.([]DiscoveredSite), func(s DiscoveredSite) bool {
		return s.Domain == domain && s.Root == webRoot && s.Managed && s.ConfigFile == vhost
	}) {
		return agent.Errorf(agent.CodeInvalidParams, "Layte Panel doesn't manage the nginx config of %s", domain)
	}
	return nil
}

// lineDiff is a minimal line diff (longest common subsequence).
func lineDiff(a, b string) []string {
	x := strings.Split(strings.TrimSuffix(a, "\n"), "\n")
	y := strings.Split(strings.TrimSuffix(b, "\n"), "\n")
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, "  "+x[i])
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "- "+x[i])
			i++
		default:
			out = append(out, "+ "+y[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+x[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+y[j])
	}
	return out
}

// nginxWorkers returns the PIDs of nginx's worker processes (children of the
// master in /run/nginx.pid). Empty if nginx's PID file can't be read.
func (d Deps) nginxWorkers() []int {
	b, err := d.FS.ReadFile("/run/nginx.pid")
	if err != nil {
		return nil
	}
	master := strings.TrimSpace(string(b))
	var out []int
	dirs, _ := d.FS.Glob("/proc/[0-9]*")
	for _, dir := range dirs {
		st, err := d.FS.ReadFile(dir + "/stat")
		if err != nil {
			continue
		}
		// pid (comm) state ppid …; comm may contain spaces and parentheses.
		i := strings.LastIndexByte(string(st), ')')
		if i < 0 {
			continue
		}
		if f := strings.Fields(string(st)[i+1:]); len(f) > 1 && f[1] == master {
			if pid, err := strconv.Atoi(path.Base(dir)); err == nil {
				out = append(out, pid)
			}
		}
	}
	return out
}

// waitNginxReloaded waits until the worker processes that ran before a
// reload no longer take new connections. `systemctl reload nginx` returns as
// soon as nginx got the signal; until the master has started new workers,
// old ones still serve the old config, so a check right after the reload
// could test the old config.
//
// An old worker is done when it has exited or is "shutting down": nginx
// tells old workers to stop only after the new ones run, and from then on
// they just finish their open connections. Those may stay open for minutes
// (live streams, keep-alive behind a CDN), so waiting for them to exit
// would time out.
func (d Deps) waitNginxReloaded(ctx context.Context, before []int) bool {
	return d.waitNginxWorkers(ctx, before, false, 15*time.Second)
}

// waitNginxDrained waits (at most timeout) until the old workers have
// exited, i.e. finished every request they had in progress.
func (d Deps) waitNginxDrained(ctx context.Context, before []int, timeout time.Duration) bool {
	return d.waitNginxWorkers(ctx, before, true, timeout)
}

func (d Deps) waitNginxWorkers(ctx context.Context, before []int, untilExit bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		alive := false
		for _, pid := range before {
			cmd, err := d.FS.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
			if err == nil && (untilExit || !strings.Contains(string(cmd), "shutting down")) {
				alive = true
				break
			}
		}
		if !alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
