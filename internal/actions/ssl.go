package actions

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

const letsencryptLive = "/etc/letsencrypt/live"

// deployHook reloads nginx after certbot renews any certificate. certbot
// runs every executable in renewal-hooks/deploy/ after a successful renewal.
const (
	deployHookPath = "/etc/letsencrypt/renewal-hooks/deploy/layte-reload-nginx"
	deployHook     = "#!/bin/sh\n" + system.Marker + " — reloads nginx after certbot renews a certificate\nexec /usr/bin/systemctl reload nginx\n"
)

// Certificate describes one certificate found on the server.
type Certificate struct {
	Name      string    `json:"name"` // certbot cert name (directory in live/)
	Domains   []string  `json:"domains"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	Managed   bool      `json:"managed"` // issued by Layte ("layte-" prefix)
	Path      string    `json:"path"`    // fullchain.pem
}

// sslList reads every certificate certbot manages (read-only).
func (d Deps) sslList(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	dirs, err := d.FS.Glob(letsencryptLive + "/*/cert.pem")
	if err != nil {
		return nil, err
	}
	out := []Certificate{}
	for _, f := range dirs {
		b, err := d.FS.ReadFile(f)
		if err != nil {
			continue
		}
		cert, err := parseCert(b)
		if err != nil {
			continue
		}
		name := path.Base(path.Dir(f))
		out = append(out, Certificate{
			Name: name, Domains: cert.DNSNames, Issuer: cert.Issuer.CommonName,
			NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC(),
			Managed: strings.HasPrefix(name, "layte-"), Path: path.Dir(f) + "/fullchain.pem",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out, nil
}

func parseCert(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

type SSLIssueParams struct {
	Domain   string   `json:"domain"`
	Aliases  []string `json:"aliases,omitempty"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
}

func (p SSLIssueParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if _, err := validate.Domains(p.Domain, p.Aliases); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	return validate.Email(p.Email)
}

// certbotBin finds certbot (apt or snap install).
func (d Deps) certbotBin() (string, error) {
	for _, p := range []string{"/usr/bin/certbot", "/snap/bin/certbot"} {
		if ok, _ := d.FS.Exists(p); ok {
			return p, nil
		}
	}
	return "", agent.Errorf(agent.CodeNotFound, "certbot is not installed (apt install certbot)")
}

// sslIssue gets a Let's Encrypt certificate with certbot's webroot method:
// certbot only drops a challenge file into the site's public/ folder, which
// the existing HTTP vhost serves. certbot never edits nginx configs here;
// Layte adds the HTTPS block to its own vhost afterwards.
func (d Deps) sslIssue(ctx context.Context, _ agent.Actor, p SSLIssueParams) (any, error) {
	vhost, err := d.FS.ReadFile(templates.VhostPath(p.Domain))
	if err != nil || !system.IsManaged(vhost) {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s has no Layte-managed nginx config", p.Domain)
	}
	certbot, err := d.certbotBin()
	if err != nil {
		return nil, err
	}
	if err := d.ensureDeployHook(); err != nil {
		return nil, err
	}
	webroot := templates.Site{Domain: p.Domain, User: p.Username}.Dir() + "/public"
	args := []string{"certonly", "--webroot", "--webroot-path", webroot,
		"--cert-name", templates.CertName(p.Domain),
		"--email", p.Email, "--agree-tos", "--no-eff-email", "--non-interactive", "--keep-until-expiring",
		"--domains", strings.Join(append([]string{p.Domain}, p.Aliases...), ",")}
	res, err := d.Runner.RunCmd(ctx, system.Cmd{Bin: certbot, Args: args})
	if err != nil {
		return nil, agent.Errorf("certbot_failed", "certbot: %s", lastLines(res.Output(), 6))
	}
	return map[string]string{"cert": templates.CertDir(p.Domain)}, nil
}

func (d Deps) ensureDeployHook() error {
	if b, err := d.FS.ReadFile(deployHookPath); err == nil && string(b) == deployHook {
		return nil
	}
	if err := d.FS.MkdirAll(path.Dir(deployHookPath), 0o755); err != nil {
		return err
	}
	return d.FS.WriteFileAtomic(deployHookPath, []byte(deployHook), 0o755)
}

// sslRevoke revokes and deletes the site's Layte certificate (idempotent).
// Certificates without the layte- prefix are never touched.
func (d Deps) sslRevoke(ctx context.Context, _ agent.Actor, p DomainParams) (any, error) {
	if ok, _ := d.FS.Exists(templates.CertDir(p.Domain)); !ok {
		return map[string]bool{"revoked": false}, nil
	}
	certbot, err := d.certbotBin()
	if err != nil {
		return nil, err
	}
	res, err := d.Runner.Run(ctx, certbot, "revoke", "--cert-name", templates.CertName(p.Domain),
		"--non-interactive", "--delete-after-revoke", "--reason", "cessationofoperation")
	if err != nil {
		return nil, agent.Errorf("certbot_failed", "certbot revoke: %s", lastLines(res.Output(), 6))
	}
	return map[string]bool{"revoked": true}, nil
}

type WPURLParams struct {
	Domain     string `json:"domain"`
	Username   string `json:"username"`
	PHPVersion string `json:"php_version"`
	URL        string `json:"url"`
}

func (p WPURLParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if err := validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion}); err != nil {
		return err
	}
	if p.URL != "http://"+p.Domain && p.URL != "https://"+p.Domain {
		return fmt.Errorf("url must be http(s)://%s", p.Domain)
	}
	return nil
}

// wpSetURL updates WordPress's home and siteurl (e.g. to https://), as the site user.
func (d Deps) wpSetURL(ctx context.Context, _ agent.Actor, p WPURLParams) (any, error) {
	cred, err := d.Users.Lookup(p.Username)
	if err != nil {
		return nil, err
	}
	phar, err := d.WPCLI.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	site := templates.Site{Domain: p.Domain, User: p.Username}
	for _, opt := range []string{"home", "siteurl"} {
		res, err := d.Runner.RunCmd(ctx, system.Cmd{
			Bin:  phpBin(p.PHPVersion),
			Args: []string{phar, "--path=" + site.Dir() + "/public", "option", "update", opt, p.URL},
			Env:  []string{"HOME=" + site.Home()},
			Dir:  site.Dir(),
			As:   &cred,
		})
		if err != nil {
			return nil, fmt.Errorf("wp option update %s: %s", opt, lastLines(res.Output(), 3))
		}
	}
	return map[string]string{"url": p.URL}, nil
}
