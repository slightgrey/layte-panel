// Package templates renders the config files Layte Panel writes for a site
// (nginx vhost, PHP-FPM pool) and knows where each one lives.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/redirects"
)

//go:embed *.tmpl
var files embed.FS

var tmpl = template.Must(template.ParseFS(files, "*.tmpl"))

// Site is everything the templates need. All fields must already be validated
// (domain, username, PHP version) — templates don't escape anything.
type Site struct {
	Domain     string
	Aliases    []string
	User       string
	PHPVersion string
	SSL        bool   // serve HTTPS with the Let's Encrypt cert CertName()
	WebUser    string // nginx's worker user, owns the FPM socket ("" = www-data)
	PHP        phpini.Settings
	Cache      pagecache.Settings // page cache (nginx FastCGI cache)
	// Redirects are exact-path redirects (validated by redirects.Clean).
	Redirects []redirects.Redirect
	// OwnCert is a certbot live folder to use instead of the Layte
	// certificate's: an adopted site keeps its own certificate until it has
	// a Layte one. "" = CertDir(Domain).
	OwnCert string
	// WebRoot is an adopted site's own web root (its files stay where they
	// were). "" for sites Layte created: Dir()/public.
	WebRoot string
	// HTTP2On: nginx 1.25.1+ turns HTTP/2 on with "http2 on;" and warns
	// about the older "listen … http2" form (which 1.24 and older need).
	HTTP2On bool
}

// CacheDir is the site's page cache folder; CacheZone its nginx zone name.
func (s Site) CacheDir() string  { return pagecache.Dir(s.Domain) }
func (s Site) CacheZone() string { return pagecache.Zone(s.Domain) }

// CertName is the certbot certificate name Layte uses for the site. The
// "layte-" prefix keeps it apart from certificates the admin made by hand.
func (s Site) CertName() string { return CertName(s.Domain) }

// CertLive is the certbot live folder the vhost's HTTPS block uses.
func (s Site) CertLive() string {
	if s.OwnCert != "" {
		return s.OwnCert
	}
	return CertDir(s.Domain)
}

// CertName for a domain.
func CertName(domain string) string { return "layte-" + domain }

// CertDir is where certbot puts the live certificate files.
func CertDir(domain string) string { return "/etc/letsencrypt/live/" + CertName(domain) }

// Home is the site user's home directory.
func (s Site) Home() string { return "/home/" + s.User }

// Dir is the site's directory: public/, logs/, tmp/ live under it.
func (s Site) Dir() string { return s.Home() + "/" + s.Domain }

// Public is the folder nginx serves.
func (s Site) Public() string {
	if s.WebRoot != "" {
		return s.WebRoot
	}
	return s.Dir() + "/public"
}

// DataDir holds the site's logs/ and tmp/: Dir() for sites Layte created,
// the user's home for adopted ones (outside the web root, never served).
func (s Site) DataDir() string {
	if s.WebRoot != "" {
		return s.Home()
	}
	return s.Dir()
}

// BaseDirs is PHP's open_basedir: the folders the site's PHP may open.
func (s Site) BaseDirs() string {
	if s.WebRoot != "" {
		return s.WebRoot + "/:" + s.Home() + "/"
	}
	return s.Dir() + "/"
}

// Socket is the PHP-FPM socket. It includes the PHP version so that while
// switching versions the old and new pools never fight over one socket.
func (s Site) Socket() string {
	return fmt.Sprintf("/run/php/layte-%s-php%s.sock", s.Domain, s.PHPVersion)
}

// Config file locations. A vhost normally lives in sites-available with a
// sites-enabled link; on servers whose nginx.conf only includes conf.d it
// lives at VhostConfD instead (the agent picks, see actions.vhostFiles).
func VhostPath(domain string) string  { return "/etc/nginx/sites-available/layte-" + domain + ".conf" }
func VhostLink(domain string) string  { return "/etc/nginx/sites-enabled/layte-" + domain + ".conf" }
func VhostConfD(domain string) string { return "/etc/nginx/conf.d/layte-" + domain + ".conf" }
func PoolPath(phpVersion, domain string) string {
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d/layte-%s.conf", phpVersion, domain)
}

// RedirectsDir holds one file of redirects per site, included by its vhost
// (outside conf.d and sites-enabled, so nginx never loads them on their own).
const RedirectsDir = "/etc/nginx/layte-redirects"

// RedirectsPath is the site's redirects file.
func RedirectsPath(domain string) string { return RedirectsDir + "/" + domain + ".conf" }

// RedirectsFile is the site's redirects file (for the vhost's include).
func (s Site) RedirectsFile() string { return RedirectsPath(s.Domain) }

// Redirects renders the site's redirects file (location blocks).
func Redirects(s Site) ([]byte, error) { return render("nginx-redirects.conf.tmpl", s) }

// Vhost renders the nginx server block.
func Vhost(s Site) ([]byte, error) { return render("nginx-site.conf.tmpl", s) }

// Pool renders the PHP-FPM pool.
func Pool(s Site) ([]byte, error) { return render("fpm-pool.conf.tmpl", s) }

func render(name string, s Site) ([]byte, error) {
	if s.Domain == "" || s.User == "" || s.PHPVersion == "" {
		return nil, fmt.Errorf("render %s: incomplete site %+v", name, s)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, s); err != nil {
		return nil, fmt.Errorf("render %s for %s: %w", name, s.Domain, err)
	}
	return buf.Bytes(), nil
}
