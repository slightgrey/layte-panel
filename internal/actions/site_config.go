package actions

import (
	"context"
	"errors"
	"fmt"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/redirects"
	"path"
	"regexp"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// SiteConfigParams describe a managed site's config files.
type SiteConfigParams struct {
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases,omitempty"`
	Username   string   `json:"username"`
	PHPVersion string   `json:"php_version"`
	SSL        bool     `json:"ssl,omitempty"`
	// PHP settings for the pool (and the vhost's upload size / timeout).
	PHP phpini.Settings `json:"php,omitempty"`
	// Page cache (nginx FastCGI cache) in the vhost.
	Cache pagecache.Settings `json:"cache,omitempty"`
	// WebRoot: an adopted site whose nginx config Layte took over (its files
	// stay there). The agent checks it against the server's config.
	WebRoot string `json:"web_root,omitempty"`
	// Redirects in the vhost (normalised; see package redirects).
	Redirects []redirects.Redirect `json:"redirects,omitempty"`
	// OwnCert: a taken-over adopted site that still uses its own certbot
	// certificate (live folder), until it gets a Layte one.
	OwnCert string `json:"own_cert,omitempty"`
	// BlockShell disables PHP's shell functions in the pool.
	BlockShell bool `json:"block_shell,omitempty"`
}

// validOwnCert checks a certbot live folder (/etc/letsencrypt/live/<name>).
func validOwnCert(dir string) error {
	name, ok := strings.CutPrefix(dir, letsencryptLive+"/")
	if !ok || !certNameRe.MatchString(name) || strings.Contains(name, "..") || strings.Contains(name, "/") {
		return fmt.Errorf("certificate folder %s is not a certbot live folder", dir)
	}
	return nil
}

// requireCertFiles checks that the live folder has the certificate and key.
func (d Deps) requireCertFiles(domain, dir string) error {
	for _, f := range []string{"fullchain.pem", "privkey.pem"} {
		if ok, _ := d.FS.Exists(dir + "/" + f); !ok {
			return agent.Errorf(agent.CodeInvalidParams, "no certificate for %s yet (%s/%s missing)", domain, dir, f)
		}
	}
	return nil
}

func (p SiteConfigParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	aliases, err := validate.Domains(p.Domain, p.Aliases)
	if err != nil {
		return err
	}
	for i := range aliases {
		if aliases[i] != p.Aliases[i] {
			return fmt.Errorf("alias %s must be in normalised form", p.Aliases[i])
		}
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.WebRoot != "" {
		if err := (FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}).validate(readOps); err != nil {
			return err
		}
	}
	if p.OwnCert != "" {
		if p.WebRoot == "" {
			return errors.New("own_cert is only for adopted sites")
		}
		if err := validOwnCert(p.OwnCert); err != nil {
			return err
		}
	}
	if err := p.PHP.Validate(); err != nil {
		return err
	}
	if err := p.Cache.Validate(); err != nil {
		return err
	}
	if err := redirects.IsClean(p.Redirects); err != nil {
		return err
	}
	if p.PHP != p.PHP.Normalize() {
		return fmt.Errorf("php settings must be in normalised form")
	}
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion}) // format; installed is checked in the handler
}

func (p SiteConfigParams) site() templates.Site {
	return templates.Site{Domain: p.Domain, Aliases: p.Aliases, User: p.Username, PHPVersion: p.PHPVersion, SSL: p.SSL, PHP: p.PHP, Cache: p.Cache, WebRoot: p.WebRoot, OwnCert: p.OwnCert, Redirects: p.Redirects, BlockShell: p.BlockShell}
}

// checkWebRoot: config writes for an adopted site need its vhost taken over.
func (d Deps) checkWebRoot(ctx context.Context, p SiteConfigParams) error {
	if p.WebRoot == "" {
		return nil
	}
	if err := d.verifyTakenOver(ctx, p.Domain, p.Username, p.WebRoot); err != nil {
		return err
	}
	if p.OwnCert != "" {
		// Only the certificate the site's config already uses.
		site, err := d.discoveredSite(ctx, p.Domain)
		if err != nil {
			return err
		}
		if path.Dir(site.CertPath) != p.OwnCert {
			return agent.Errorf(agent.CodeInvalidParams, "%s doesn't use the certificate in %s", p.Domain, p.OwnCert)
		}
	}
	return nil
}

// requireInstalledPHP checks the version against what's really installed.
func (d Deps) requireInstalledPHP(v string) error {
	installed, err := d.phpVersions()
	if err != nil {
		return err
	}
	if err := validate.PHPVersion(v, installed); err != nil {
		return agent.Errorf(agent.CodeInvalidParams, "%v", err)
	}
	return nil
}

func fpmTest(v string) system.Cmd { return system.Cmd{Bin: phpFPMBin(v), Args: []string{"-t"}} }
func fpmReload(v string) system.Cmd {
	return system.Cmd{Bin: systemctl, Args: []string{"reload", phpFPMUnit(v)}}
}

var (
	nginxTest   = system.Cmd{Bin: nginxBin, Args: []string{"-t"}}
	nginxReload = system.Cmd{Bin: systemctl, Args: []string{"reload", "nginx"}}
)

// phpPoolWrite renders and safely installs the site's PHP-FPM pool.
func (d Deps) phpPoolWrite(ctx context.Context, _ agent.Actor, p SiteConfigParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	if err := d.checkWebRoot(ctx, p); err != nil {
		return nil, err
	}
	if _, err := d.Users.Lookup(p.Username); err != nil {
		return nil, err
	}
	// Before the pool's reload, so later reloads don't cut off requests
	// (e.g. a PHP version installed after the agent started).
	if _, err := d.ensureFPMGraceful(ctx, p.PHPVersion); err != nil {
		return nil, err
	}
	site := p.site()
	site.WebUser = d.webUser() // the socket must be reachable by nginx's workers
	content, err := templates.Pool(site)
	if err != nil {
		return nil, err
	}
	path := templates.PoolPath(p.PHPVersion, p.Domain)
	err = system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: path, Content: content, Test: fpmTest(p.PHPVersion), Reload: fpmReload(p.PHPVersion),
	})
	if err != nil {
		return nil, configError(err)
	}
	// Callers point nginx at the pool next (create, PHP switch): only once it serves.
	if err := d.waitFPMSocket(ctx, site.Socket()); err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}

type PoolRemoveParams struct {
	Domain     string `json:"domain"`
	PHPVersion string `json:"php_version"`
}

func (p PoolRemoveParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion})
}

// phpPoolRemove removes the site's pool for one PHP version (idempotent).
func (d Deps) phpPoolRemove(ctx context.Context, _ agent.Actor, p PoolRemoveParams) (any, error) {
	path := templates.PoolPath(p.PHPVersion, p.Domain)
	if ok, _ := d.FS.Exists(path); !ok {
		return map[string]bool{"removed": false}, nil
	}
	err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{
		Path: path, Test: fpmTest(p.PHPVersion), Reload: fpmReload(p.PHPVersion),
	})
	if err != nil {
		return nil, configError(err)
	}
	return map[string]bool{"removed": true}, nil
}

// sitesEnabledInclude matches an active (not commented) include of sites-enabled.
var sitesEnabledInclude = regexp.MustCompile(`(?m)^[ \t]*include[ \t]+(/etc/nginx/)?sites-enabled/`)

// vhostFiles says where the site's vhost lives, plus its sites-enabled link
// ("" for none). An existing vhost stays where it is. A new one goes to
// sites-available + a sites-enabled link, unless nginx.conf doesn't include
// sites-enabled: then to conf.d. (A file nginx never loads still passes
// `nginx -t`, so the site would silently not be served.)
func (d Deps) vhostFiles(domain string) (path, link string) {
	confd := templates.VhostConfD(domain)
	if ok, _ := d.FS.Exists(confd); ok {
		return confd, ""
	}
	if ok, _ := d.FS.Exists(templates.VhostPath(domain)); ok {
		return templates.VhostPath(domain), templates.VhostLink(domain)
	}
	if b, err := d.FS.ReadFile(nginxDir + "/nginx.conf"); err == nil && !sitesEnabledInclude.Match(b) {
		return confd, ""
	}
	return templates.VhostPath(domain), templates.VhostLink(domain)
}

// nginxVhostWrite renders and safely installs + enables the site's vhost.
func (d Deps) nginxVhostWrite(ctx context.Context, _ agent.Actor, p SiteConfigParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	if err := d.checkWebRoot(ctx, p); err != nil {
		return nil, err
	}
	if p.SSL {
		if err := d.requireCertFiles(p.Domain, p.site().CertLive()); err != nil {
			return nil, err
		}
	}
	vsite := p.site()
	vsite.HTTP2On = d.nginxHTTP2On(ctx)
	content, err := templates.Vhost(vsite)
	if err != nil {
		return nil, err
	}
	if p.Cache.Enabled {
		// nginx creates the site's cache folder itself, but not its parents.
		if err := d.FS.MkdirAll(pagecache.Root, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", pagecache.Root, err)
		}
	}
	// Redirects live in their own file, which the vhost includes. With
	// redirects, that file is written first (the vhost's nginx -t then checks
	// it); if the vhost can't be written, the file is put back as it was.
	// Without, the file is removed after the vhost no longer includes it.
	rpath := templates.RedirectsPath(p.Domain)
	restoreRedirects := func() {}
	if len(p.Redirects) > 0 {
		if restoreRedirects, err = d.writeRedirects(ctx, rpath, p.site()); err != nil {
			return nil, configError(err)
		}
	}
	path, link := d.vhostFiles(p.Domain)
	err = system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: path, Content: content, Link: link, Test: nginxTest, Reload: nginxReload,
	})
	if err != nil {
		restoreRedirects()
		return nil, configError(err)
	}
	if len(p.Redirects) == 0 {
		if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: rpath, Test: nginxTest, Reload: nginxReload}); err != nil {
			return nil, configError(err)
		}
	}
	return map[string]string{"path": path}, nil
}

// writeRedirects installs the site's redirects file and returns a function
// that puts the previous one back (or removes it if there was none).
func (d Deps) writeRedirects(ctx context.Context, rpath string, site templates.Site) (func(), error) {
	content, err := templates.Redirects(site)
	if err != nil {
		return nil, err
	}
	if err := d.FS.MkdirAll(templates.RedirectsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", templates.RedirectsDir, err)
	}
	old, oldErr := d.FS.ReadFile(rpath)
	spec := system.WriteSpec{Path: rpath, Content: content, Test: nginxTest, Reload: nginxReload}
	if err := system.WriteConfig(ctx, d.FS, d.Runner, spec); err != nil {
		return nil, err
	}
	bg := context.WithoutCancel(ctx)
	return func() {
		if oldErr == nil {
			spec.Content = old
			system.WriteConfig(bg, d.FS, d.Runner, spec)
		} else {
			system.RemoveConfig(bg, d.FS, d.Runner, system.RemoveSpec{Path: rpath, Test: nginxTest, Reload: nginxReload})
		}
	}, nil
}

type DomainParams struct {
	Domain string `json:"domain"`
}

func (p DomainParams) Validate() error { return validDomain(p.Domain) }

// nginxVhostRemove disables and removes the site's vhost (idempotent).
func (d Deps) nginxVhostRemove(ctx context.Context, _ agent.Actor, p DomainParams) (any, error) {
	path, link := d.vhostFiles(p.Domain)
	err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{
		Path: path, Link: link, Test: nginxTest, Reload: nginxReload,
	})
	if err != nil {
		return nil, configError(err)
	}
	// The site's redirects file (nothing includes it any more).
	if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: templates.RedirectsPath(p.Domain)}); err != nil {
		return nil, configError(err)
	}
	return map[string]bool{"removed": true}, nil
}

// configError turns config-test failures into a typed agent error with the
// tool's output, so the UI can show what nginx/php-fpm complained about.
func configError(err error) error {
	var te *system.TestError
	if errors.As(err, &te) {
		return agent.Errorf("config_test_failed", "%s (change rolled back)", te.Error())
	}
	return err
}
