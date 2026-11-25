package actions

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Adopting an imported site: Layte starts managing it where it is, without
// touching its nginx vhost. It gets its own new Linux user (with the Layte
// marker), its files change owner from the old PHP user to the new one, and
// its PHP-FPM pool runs as the new user. The pool file is the only config
// Layte takes over (the original is kept as <pool>.layte-orig).

const chownBin = "/usr/bin/chown"

// sudoGroups make a user root-equivalent on Ubuntu/Debian.
var sudoGroups = []string{"sudo", "admin", "wheel", "root"}

// AdoptPreviewParams name an imported site by domain and web root.
type AdoptPreviewParams struct {
	Domain  string `json:"domain"`
	WebRoot string `json:"web_root"`
}

func (p AdoptPreviewParams) Validate() error {
	return FSParams{Domain: p.Domain, WebRoot: p.WebRoot, Op: "stat", Path: "."}.validate(readOps)
}

// AdoptParams: the same plus the new user and the pool content the admin saw.
type AdoptParams struct {
	Domain   string `json:"domain"`
	WebRoot  string `json:"web_root"`
	Username string `json:"username"`
	PoolHash string `json:"pool_hash"` // from the preview: refuses if the pool changed since
}

func (p AdoptParams) Validate() error {
	if err := (AdoptPreviewParams{Domain: p.Domain, WebRoot: p.WebRoot}).Validate(); err != nil {
		return err
	}
	if p.PoolHash == "" {
		return errors.New("pool_hash from the preview is required")
	}
	return validate.SiteUsername(p.Username)
}

// AdoptPreview is what adopting would change. Nothing is changed to make it.
type AdoptPreview struct {
	Domain     string   `json:"domain"`
	WebRoot    string   `json:"web_root"`
	PHPVersion string   `json:"php_version"`
	PoolFile   string   `json:"pool_file"`
	PoolName   string   `json:"pool_name"`
	OldUser    string   `json:"old_user"`
	OldGroup   string   `json:"old_group"`
	Unsafe     []string `json:"unsafe,omitempty"` // why the old user mustn't be used (informational)
	Blockers   []string `json:"blockers,omitempty"`
	PoolDiff   []string `json:"pool_diff"` // "+ line" / "- line" / "  line"
	PoolHash   string   `json:"pool_hash"`
	LogFiles   []string `json:"log_files,omitempty"` // PHP logs that change owner too
}

// poolFile is a parsed single-pool PHP-FPM config.
type poolFile struct {
	Path, Version, Name, User, Group, Listen string
	Content                                  string
	Logs                                     []string
}

var poolLogKeys = regexp.MustCompile(`^(php_admin_value|php_value)\[error_log\]$|^slowlog$|^access\.log$`)

func parsePool(file, version, content string) (poolFile, int) {
	p := poolFile{Path: file, Version: version, Content: content}
	sections := 0
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections++
			p.Name = strings.Trim(line, "[]")
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		switch {
		case k == "user":
			p.User = v
		case k == "group":
			p.Group = v
		case k == "listen":
			p.Listen = v
		case poolLogKeys.MatchString(k):
			if strings.HasPrefix(v, "/") && !strings.Contains(v, "$") {
				p.Logs = append(p.Logs, path.Clean(v))
			}
		}
	}
	return p, sections
}

// findPools returns every pool file that listens on socket.
func (d Deps) findPools(socket string) []poolFile {
	var out []poolFile
	files, _ := d.FS.Glob("/etc/php/*/fpm/pool.d/*.conf")
	for _, f := range files {
		version := path.Base(path.Dir(path.Dir(path.Dir(f))))
		if !phpVersionRe.MatchString(version) {
			continue
		}
		b, err := d.FS.ReadFile(f)
		if err != nil {
			continue
		}
		p, sections := parsePool(f, version, string(b))
		if p.Listen != "" && p.Listen == socket { // files without listen (e.g. [global] only) are no pools
			if sections != 1 {
				p.Name = "" // marks "more than one pool in this file"
			}
			out = append(out, p)
		}
	}
	return out
}

// adoptTarget checks that domain + web root are an imported site with its
// own PHP-FPM pool, all from the server's own config (nothing from the API
// is trusted).
func (d Deps) adoptTarget(ctx context.Context, domain, webRoot string) (DiscoveredSite, poolFile, []string, error) {
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return DiscoveredSite{}, poolFile{}, nil, err
	}
	all := out.([]DiscoveredSite)
	i := slices.IndexFunc(all, func(s DiscoveredSite) bool { return s.Domain == domain && s.Root == webRoot })
	if i < 0 {
		return DiscoveredSite{}, poolFile{}, nil, agent.Errorf(agent.CodeNotFound, "%s with web root %s isn't in the nginx config", domain, webRoot)
	}
	site := all[i]
	var blockers []string
	if site.Managed {
		return site, poolFile{}, nil, agent.Errorf(agent.CodeInvalidParams, "%s is already managed by Layte Panel", domain)
	}
	if site.PHPSocket == "" {
		return site, poolFile{}, nil, agent.Errorf(agent.CodeInvalidParams, "%s doesn't use a PHP-FPM unix socket, so there is no pool to adopt", domain)
	}
	var others []string
	for _, s := range all {
		if s.PHPSocket == site.PHPSocket && s.Domain != domain {
			others = append(others, s.Domain)
		}
	}
	if len(others) > 0 {
		blockers = append(blockers, fmt.Sprintf("its PHP-FPM pool (%s) is shared with %s; changing it would affect those sites too", site.PHPSocket, strings.Join(others, ", ")))
	}
	pools := d.findPools(site.PHPSocket)
	switch {
	case len(pools) == 0:
		return site, poolFile{}, nil, agent.Errorf(agent.CodeNotFound, "no PHP-FPM pool file listens on %s", site.PHPSocket)
	case len(pools) > 1:
		var names []string
		for _, p := range pools {
			names = append(names, p.Path)
		}
		return site, poolFile{}, nil, agent.Errorf(agent.CodeInvalidParams, "several pool files listen on %s: %s", site.PHPSocket, strings.Join(names, ", "))
	}
	pool := pools[0]
	if pool.Name == "" {
		blockers = append(blockers, pool.Path+" defines more than one pool")
	}
	if pool.Name == "www" || path.Base(pool.Path) == "www.conf" {
		blockers = append(blockers, "it uses PHP's default pool (www), which any site can point at; give the site its own pool first")
	}
	if system.IsManaged([]byte(pool.Content)) {
		blockers = append(blockers, pool.Path+" is already managed by Layte Panel")
	}
	if pool.User == "" || pool.User == "root" {
		blockers = append(blockers, "the pool has no user, or runs as root")
	}
	if pool.Group == "" {
		pool.Group = pool.User
	}
	return site, pool, blockers, nil
}

// unsafeReasons says why a user mustn't run a panel-managed site.
func (d Deps) unsafeReasons(name string) []string {
	var out []string
	if cred, err := d.Users.Lookup(name); err == nil && cred.UID < 1000 {
		out = append(out, fmt.Sprintf("%s is a system account (uid %d)", name, cred.UID))
	}
	if b, err := d.FS.ReadFile("/etc/group"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Split(line, ":")
			if len(f) == 4 && slices.Contains(sudoGroups, f[0]) && slices.Contains(strings.Split(f[3], ","), name) {
				out = append(out, fmt.Sprintf("%s is in the %s group (can become root)", name, f[0]))
			}
		}
	}
	if ok, _ := d.FS.Exists("/home/" + name + "/.ssh/authorized_keys"); ok {
		out = append(out, name+" can log in over SSH")
	}
	return out
}

// newPoolContent swaps the pool's user and group lines and adds the marker.
func newPoolContent(p poolFile, user string) (string, []string) {
	var b strings.Builder
	header := system.MarkerINI + " — adopted for " + user + "; original kept as " + path.Base(p.Path) + system.AdoptSuffix
	b.WriteString(header + "\n")
	diff := []string{"+ " + header}
	for _, raw := range strings.Split(strings.TrimSuffix(p.Content, "\n"), "\n") {
		k, _, ok := strings.Cut(raw, "=")
		key := strings.TrimSpace(k)
		if ok && (key == "user" || key == "group") && !strings.HasPrefix(strings.TrimSpace(raw), ";") {
			repl := key + " = " + user
			b.WriteString(repl + "\n")
			diff = append(diff, "- "+raw, "+ "+repl)
			continue
		}
		b.WriteString(raw + "\n")
		diff = append(diff, "  "+raw)
	}
	if !strings.Contains(p.Content, "\ngroup") && !strings.HasPrefix(p.Content, "group") {
		// no group line: FPM would use the user's primary group, which is the same
	}
	return b.String(), diff
}

// poolHash identifies the pool content the preview showed.
func poolHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

func (d Deps) adoptPreview(ctx context.Context, _ agent.Actor, p AdoptPreviewParams) (any, error) {
	site, pool, blockers, err := d.adoptTarget(ctx, p.Domain, p.WebRoot)
	if err != nil {
		return nil, err
	}
	_, diff := newPoolContent(pool, "<new user>")
	return AdoptPreview{
		Domain: site.Domain, WebRoot: site.Root, PHPVersion: pool.Version, PoolFile: pool.Path, PoolName: pool.Name,
		OldUser: pool.User, OldGroup: pool.Group, Unsafe: d.unsafeReasons(pool.User), Blockers: blockers,
		PoolDiff: diff, PoolHash: poolHash(pool.Content), LogFiles: d.ownedLogs(pool),
	}, nil
}

// ownedLogs are the pool's log files that exist as regular files owned by
// the old user (only those change owner).
func (d Deps) ownedLogs(pool poolFile) []string {
	var out []string
	old, err := d.Users.Lookup(pool.User)
	for _, l := range pool.Logs {
		fi, e := d.FS.Lstat(l)
		if e != nil || !fi.Mode().IsRegular() {
			continue
		}
		if owner, ok := system.Owner(fi); ok && err == nil && owner.UID != old.UID {
			continue
		}
		out = append(out, l)
	}
	return out
}

// AdoptResult is returned by site.adopt.
type AdoptResult struct {
	Username   string   `json:"username"`
	PoolFile   string   `json:"pool_file"`
	PHPVersion string   `json:"php_version"`
	OldUser    string   `json:"old_user"`
	Steps      []string `json:"steps"`
}

// siteAdopt does the whole adoption, undoing every finished step if a later
// one fails (or if the site answers differently afterwards).
func (d Deps) siteAdopt(ctx context.Context, actor agent.Actor, p AdoptParams) (any, error) {
	site, pool, blockers, err := d.adoptTarget(ctx, p.Domain, p.WebRoot)
	if err != nil {
		return nil, err
	}
	if len(blockers) > 0 {
		return nil, agent.Errorf(agent.CodeInvalidParams, "can't adopt %s: %s", p.Domain, strings.Join(blockers, "; "))
	}
	if poolHash(pool.Content) != p.PoolHash {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s changed since the preview; review it again", pool.Path)
	}
	if p.Username == pool.User {
		return nil, agent.Errorf(agent.CodeInvalidParams, "the new user must differ from the current one")
	}
	if _, err := d.Users.Lookup(p.Username); err == nil {
		return nil, agent.Errorf(agent.CodeInvalidParams, "Linux user %q already exists; adoption needs a new user", p.Username)
	}
	before, err := d.siteStatus(ctx, site)
	if err != nil {
		return nil, agent.Errorf("site_check_failed", "%s doesn't answer before adopting (%v); fix that first", p.Domain, err)
	}

	res := AdoptResult{Username: p.Username, PoolFile: pool.Path, PHPVersion: pool.Version, OldUser: pool.User}
	var undo []func()
	fail := func(err error) (any, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}
	step := func(s string) { res.Steps = append(res.Steps, s) }
	bg := context.WithoutCancel(ctx)

	// 1. New user (+ nginx in its group) and its data folder for dumps and cron logs.
	if _, err := d.siteUserCreate(ctx, actor, UserParams{Username: p.Username}); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { d.siteUserRemove(bg, actor, UserParams{Username: p.Username}) })
	step("Created Linux user " + p.Username)
	if err := d.adoptedDataDirs(p.Username); err != nil {
		return fail(err)
	}

	// 2. Files: owner old → new, and group old → new, only where they match.
	chown := func(from, to string) error {
		args := []string{"--recursive", "--no-dereference", "--from=" + from, to, "--", site.Root}
		if r, err := d.Runner.Run(ctx, chownBin, args...); err != nil {
			return fmt.Errorf("chown %s: %s", site.Root, failure(r, err, 3))
		}
		return nil
	}
	if err := chown(pool.User, p.Username); err != nil {
		return fail(err)
	}
	undo = append(undo, func() {
		d.Runner.Run(bg, chownBin, "--recursive", "--no-dereference", "--from="+p.Username, pool.User, "--", site.Root)
	})
	if err := chown(":"+pool.Group, ":"+p.Username); err != nil {
		return fail(err)
	}
	undo = append(undo, func() {
		d.Runner.Run(bg, chownBin, "--recursive", "--no-dereference", "--from=:"+p.Username, ":"+pool.Group, "--", site.Root)
	})
	step(fmt.Sprintf("Changed owner of %s from %s to %s", site.Root, pool.User, p.Username))

	for _, l := range d.ownedLogs(pool) {
		if r, err := d.Runner.Run(ctx, chownBin, "--no-dereference", p.Username+":"+p.Username, "--", l); err != nil {
			return fail(fmt.Errorf("chown %s: %s", l, failure(r, err, 3)))
		}
		l := l
		undo = append(undo, func() { d.Runner.Run(bg, chownBin, "--no-dereference", pool.User+":"+pool.Group, "--", l) })
		step("Changed owner of " + l)
	}

	// 3. The pool runs as the new user (tested, reloaded, original kept).
	content, _ := newPoolContent(pool, p.Username)
	spec := system.WriteSpec{Path: pool.Path, Content: []byte(content), Test: fpmTest(pool.Version), Reload: fpmReload(pool.Version)}
	if err := system.AdoptConfig(ctx, d.FS, d.Runner, spec, []byte(pool.Content)); err != nil {
		return fail(configError(err))
	}
	undo = append(undo, func() {
		system.RestoreAdopted(bg, d.FS, d.Runner, pool.Path, fpmTest(pool.Version), fpmReload(pool.Version))
	})
	step(fmt.Sprintf("PHP %s pool [%s] now runs as %s", pool.Version, pool.Name, p.Username))

	// 4. The site must answer exactly as before.
	after, err := d.siteStatus(ctx, site)
	if err != nil {
		return fail(agent.Errorf("site_check_failed", "%s answered %d before, and no longer answers after (%v); everything was undone", p.Domain, before, err))
	}
	if after != before {
		return fail(agent.Errorf("site_check_failed", "%s answered %d before and %d after; everything was undone", p.Domain, before, after))
	}
	step(fmt.Sprintf("%s still answers %d", p.Domain, after))
	return res, nil
}

// adoptedDataDirs creates /home/<user>/{tmp,logs}: dumps and cron logs of an
// adopted site live outside its web root, so they are never served.
func (d Deps) adoptedDataDirs(user string) error {
	cred, err := d.Users.Lookup(user)
	if err != nil {
		return err
	}
	for _, dir := range []string{"/home/" + user + "/tmp", "/home/" + user + "/logs"} {
		if err := d.FS.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		fi, err := d.FS.Lstat(dir)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("%s is not a plain directory", dir)
		}
		if err := d.FS.Chown(dir, int(cred.UID), int(cred.GID)); err != nil {
			return err
		}
	}
	return nil
}

// siteStatus fetches the site's home page over loopback (HTTPS if it has a
// certificate, since the HTTP block often only redirects) without following
// redirects, bypassing the page cache. Only the status code matters.
func (d Deps) siteStatus(ctx context.Context, site DiscoveredSite) (int, error) {
	if d.SiteCheck != nil {
		return d.SiteCheck(ctx, site.Domain, site.SSL)
	}
	return loopbackStatus(ctx, site.Domain, site.SSL)
}

func loopbackStatus(ctx context.Context, domain string, https bool) (int, error) {
	scheme, port := "http", "80"
	if https {
		scheme, port = "https", "443"
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, "127.0.0.1:"+port)
			},
			// Loopback only, and only the status code is used.
			TLSClientConfig: &tls.Config{ServerName: domain, InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// A unique query string: the page cache never answers it (and other
	// caches don't either), so PHP really runs. WordPress ignores it.
	url := fmt.Sprintf("%s://%s/?layte-check=%d", scheme, domain, time.Now().UnixNano())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "layte-adopt-check")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// verifyAdopted checks, from the server's own config, that domain + web root
// + user is an adopted site: the user is a Layte user, nginx serves the
// domain from that web root, and the pool behind it is Layte's and runs as
// that user. File, cron and dump actions on adopted sites all go through it.
// The vhost may be the admin's or, once taken over, Layte's own.
func (d Deps) verifyAdopted(ctx context.Context, domain, user, webRoot string) error {
	if ok, _ := d.FS.Exists("/home/" + user + "/" + siteUserMarker); !ok {
		return agent.Errorf(agent.CodeInvalidParams, "%s is not a Layte site user", user)
	}
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return err
	}
	all := out.([]DiscoveredSite)
	i := slices.IndexFunc(all, func(s DiscoveredSite) bool { return s.Domain == domain && s.Root == webRoot })
	if i < 0 || all[i].PHPSocket == "" {
		return agent.Errorf(agent.CodeInvalidParams, "%s is not served from %s", domain, webRoot)
	}
	pools := d.findPools(all[i].PHPSocket)
	if len(pools) != 1 || !system.IsManaged([]byte(pools[0].Content)) || pools[0].User != user {
		return agent.Errorf(agent.CodeInvalidParams, "%s's PHP pool isn't an adopted pool running as %s", domain, user)
	}
	return nil
}
