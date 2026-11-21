// Package templates renders the config files Layte Panel writes for a site
// (nginx vhost, PHP-FPM pool) and knows where each one lives.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
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
	SSL        bool // serve HTTPS with the Let's Encrypt cert CertName()
}

// CertName is the certbot certificate name Layte uses for the site. The
// "layte-" prefix keeps it apart from certificates the admin made by hand.
func (s Site) CertName() string { return CertName(s.Domain) }

// CertName for a domain.
func CertName(domain string) string { return "layte-" + domain }

// CertDir is where certbot puts the live certificate files.
func CertDir(domain string) string { return "/etc/letsencrypt/live/" + CertName(domain) }

// Home is the site user's home directory.
func (s Site) Home() string { return "/home/" + s.User }

// Dir is the site's directory: public/, logs/, tmp/ live under it.
func (s Site) Dir() string { return s.Home() + "/" + s.Domain }

// Socket is the PHP-FPM socket. It includes the PHP version so that while
// switching versions the old and new pools never fight over one socket.
func (s Site) Socket() string {
	return fmt.Sprintf("/run/php/layte-%s-php%s.sock", s.Domain, s.PHPVersion)
}

// Config file locations.
func VhostPath(domain string) string { return "/etc/nginx/sites-available/layte-" + domain + ".conf" }
func VhostLink(domain string) string { return "/etc/nginx/sites-enabled/layte-" + domain + ".conf" }
func PoolPath(phpVersion, domain string) string {
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d/layte-%s.conf", phpVersion, domain)
}

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
