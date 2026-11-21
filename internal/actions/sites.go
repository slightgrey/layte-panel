package actions

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/nginx"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// DiscoveredSite is one site found in the nginx config.
type DiscoveredSite struct {
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases"`
	Root       string   `json:"root"`
	PHPVersion string   `json:"php_version"` // "" for static sites or unknown
	SSL        bool     `json:"ssl"`
	CertPath   string   `json:"cert_path,omitempty"`
	ConfigFile string   `json:"config_file"`
	Managed    bool     `json:"managed"` // has the Layte marker
	WordPress  bool     `json:"wordpress"`
}

const nginxDir = "/etc/nginx"

var phpSocketRe = regexp.MustCompile(`php([0-9]+\.[0-9]+)-fpm\.sock$|php([0-9]+\.[0-9]+)\.sock$`)

// sitesDiscover scans enabled nginx configs (read-only) and returns sites.
func (d Deps) sitesDiscover(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	var files []string
	for _, pattern := range []string{nginxDir + "/sites-enabled/*", nginxDir + "/conf.d/*.conf"} {
		m, err := d.FS.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}
		files = append(files, m...)
	}
	pools := d.poolSockets()

	byDomain := map[string]*DiscoveredSite{}
	var order []string
	for _, f := range files {
		base := path.Base(f)
		if strings.HasPrefix(base, ".") || strings.HasSuffix(base, ".layte-bak") || strings.HasSuffix(base, "~") || base == "layte-panel.conf" {
			continue
		}
		real, err := d.FS.Resolve(f)
		if err != nil || !strings.HasPrefix(real, nginxDir+"/") {
			continue // dangling link, or a link pointing outside /etc/nginx
		}
		b, err := d.FS.ReadFile(real)
		if err != nil {
			continue
		}
		ds, err := nginx.Parse(string(b))
		if err != nil {
			continue // a broken file can't be served by nginx either
		}
		managed := system.IsManaged(b)
		for _, srv := range servers(ds) {
			site := d.siteFromServer(srv.Block, pools)
			if site == nil {
				continue
			}
			site.ConfigFile, site.Managed = real, managed
			if prev, ok := byDomain[site.Domain]; ok {
				mergeSite(prev, site) // e.g. Certbot's :80 redirect block + the :443 block
				continue
			}
			byDomain[site.Domain] = site
			order = append(order, site.Domain)
		}
	}

	out := make([]DiscoveredSite, 0, len(order))
	for _, dom := range order {
		s := byDomain[dom]
		s.WordPress = d.isWordPress(s.Root)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

// servers returns server{} blocks at top level or inside http{}.
func servers(ds []*nginx.Directive) []*nginx.Directive {
	out := nginx.Find(ds, "server")
	for _, h := range nginx.Find(ds, "http") {
		out = append(out, nginx.Find(h.Block, "server")...)
	}
	return out
}

func (d Deps) siteFromServer(block []*nginx.Directive, pools map[string]string) *DiscoveredSite {
	var names []string
	for _, sn := range nginx.Find(block, "server_name") {
		for _, n := range sn.Args {
			if n == "_" || n == "" || strings.HasPrefix(n, "~") || strings.Contains(n, "*") {
				continue
			}
			if dom, err := validate.Domain(n); err == nil {
				names = append(names, dom)
			}
		}
	}
	if len(names) == 0 {
		return nil // catch-all/default server
	}
	s := &DiscoveredSite{Domain: names[0], Aliases: dedupe(names[1:], names[0])}

	for _, l := range nginx.Find(block, "listen") {
		for _, a := range l.Args {
			if a == "ssl" || a == "443" || strings.HasSuffix(a, ":443") {
				s.SSL = true
			}
		}
	}
	if r := nginx.First(block, "root"); r != nil {
		s.Root = r.Arg(0)
	} else {
		for _, loc := range nginx.Find(block, "location") {
			if loc.Arg(0) == "/" {
				if r := nginx.First(loc.Block, "root"); r != nil {
					s.Root = r.Arg(0)
				}
			}
		}
	}
	if c := nginx.First(block, "ssl_certificate"); c != nil {
		s.CertPath = c.Arg(0)
	}
	nginx.Walk(block, func(dir *nginx.Directive) {
		if dir.Name != "fastcgi_pass" || s.PHPVersion != "" {
			return
		}
		sock := strings.TrimPrefix(dir.Arg(0), "unix:")
		if v, ok := pools[sock]; ok {
			s.PHPVersion = v
		} else if m := phpSocketRe.FindStringSubmatch(sock); m != nil {
			s.PHPVersion = m[1] + m[2]
		}
	})
	return s
}

// poolSockets maps PHP-FPM listen sockets to their PHP version by reading
// /etc/php/X.Y/fpm/pool.d/*.conf.
func (d Deps) poolSockets() map[string]string {
	out := map[string]string{}
	files, _ := d.FS.Glob("/etc/php/*/fpm/pool.d/*.conf")
	for _, f := range files {
		version := path.Base(path.Dir(path.Dir(path.Dir(f)))) // /etc/php/<v>/fpm/pool.d/x.conf
		if !phpVersionRe.MatchString(version) {
			continue
		}
		b, err := d.FS.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(k) == "listen" {
				out[strings.TrimSpace(v)] = version
			}
		}
	}
	return out
}

func (d Deps) isWordPress(root string) bool {
	if root == "" {
		return false
	}
	for _, p := range []string{path.Join(root, "wp-config.php"), path.Join(path.Dir(root), "wp-config.php")} {
		if ok, _ := d.FS.Exists(p); ok {
			return true
		}
	}
	return false
}

func mergeSite(dst, src *DiscoveredSite) {
	dst.SSL = dst.SSL || src.SSL
	if dst.Root == "" {
		dst.Root = src.Root
	}
	if dst.PHPVersion == "" {
		dst.PHPVersion = src.PHPVersion
	}
	if dst.CertPath == "" {
		dst.CertPath = src.CertPath
	}
	dst.Aliases = dedupe(append(dst.Aliases, src.Aliases...), dst.Domain)
}

func dedupe(list []string, exclude string) []string {
	out := []string{}
	seen := map[string]bool{exclude: true}
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// phpVersionsAction lists installed PHP-FPM versions.
func (d Deps) phpVersionsAction(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	v, err := d.phpVersions()
	if v == nil {
		v = []string{}
	}
	return v, err
}

// VhostReadParams is the input of nginx.vhost_read.
type VhostReadParams struct {
	Path string `json:"path"`
}

func (p VhostReadParams) Validate() error {
	clean := path.Clean(p.Path)
	if clean != p.Path || !strings.HasPrefix(clean, nginxDir+"/") {
		return fmt.Errorf("path must be a file under %s", nginxDir)
	}
	return nil
}

const maxVhostSize = 256 << 10

// vhostRead returns an nginx config file's text (read-only viewer). The real
// path, after following symlinks, must still be inside /etc/nginx.
func (d Deps) vhostRead(ctx context.Context, _ agent.Actor, p VhostReadParams) (any, error) {
	real, err := d.FS.Resolve(p.Path)
	if err != nil {
		return nil, agent.Errorf(agent.CodeNotFound, "%s: %v", p.Path, err)
	}
	if !strings.HasPrefix(real, nginxDir+"/") {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s points outside %s", p.Path, nginxDir)
	}
	fi, err := d.FS.Lstat(real)
	if err != nil {
		return nil, agent.Errorf(agent.CodeNotFound, "%s not found", p.Path)
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxVhostSize {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s is not a regular file under 256 KB", p.Path)
	}
	b, err := d.FS.ReadFile(real)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": real, "content": string(b), "managed": system.IsManaged(b)}, nil
}
