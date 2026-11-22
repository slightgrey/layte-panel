package actions

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
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

const (
	letsencryptLive    = "/etc/letsencrypt/live"
	letsencryptRenewal = "/etc/letsencrypt/renewal"
)

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

	Serial      string `json:"serial"`      // hex
	Fingerprint string `json:"fingerprint"` // SHA-256 of the DER, colon-separated hex
	KeyType     string `json:"key_type"`    // e.g. "ECDSA P-256", "RSA 2048"
	Signature   string `json:"signature"`   // e.g. "ECDSA-SHA384"
	AutoRenew   bool   `json:"auto_renew"`  // certbot has a renewal config for it
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
		renewConf, _ := d.FS.Exists(letsencryptRenewal + "/" + name + ".conf")
		out = append(out, Certificate{
			Name: name, Domains: cert.DNSNames, Issuer: cert.Issuer.CommonName,
			NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC(),
			Managed: strings.HasPrefix(name, "layte-"), Path: path.Dir(f) + "/fullchain.pem",
			Serial: fmt.Sprintf("%X", cert.SerialNumber), Fingerprint: fingerprint(cert.Raw),
			KeyType: keyType(cert), Signature: cert.SignatureAlgorithm.String(), AutoRenew: renewConf,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out, nil
}

// fingerprint is the SHA-256 of a certificate, as browsers show it.
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

func keyType(c *x509.Certificate) string {
	switch k := c.PublicKey.(type) {
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return c.PublicKeyAlgorithm.String()
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
	vhostPath, _ := d.vhostFiles(p.Domain)
	vhost, err := d.FS.ReadFile(vhostPath)
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
		return nil, agent.Errorf("certbot_failed", "certbot: %s", failure(res, err, 6))
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

type SSLRenewParams struct {
	Domain string `json:"domain"`
	DryRun bool   `json:"dry_run,omitempty"`
}

func (p SSLRenewParams) Validate() error { return validDomain(p.Domain) }

// sslRenew renews the site's Layte certificate now. DryRun asks Let's
// Encrypt's staging server instead, which checks that renewal will work
// without issuing anything. A real renewal is forced (certbot would skip a
// certificate that isn't due yet); certbot's deploy hook reloads nginx.
// Certificates without the layte- prefix are never touched.
func (d Deps) sslRenew(ctx context.Context, _ agent.Actor, p SSLRenewParams) (any, error) {
	if ok, _ := d.FS.Exists(templates.CertDir(p.Domain)); !ok {
		return nil, agent.Errorf(agent.CodeNotFound, "%s has no Layte certificate", p.Domain)
	}
	certbot, err := d.certbotBin()
	if err != nil {
		return nil, err
	}
	if err := d.ensureDeployHook(); err != nil {
		return nil, err
	}
	args := []string{"renew", "--cert-name", templates.CertName(p.Domain), "--non-interactive"}
	if p.DryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--force-renewal")
	}
	res, err := d.Runner.Run(ctx, certbot, args...)
	if err != nil {
		return nil, agent.Errorf("certbot_failed", "certbot renew: %s", failure(res, err, 6))
	}
	return map[string]bool{"renewed": !p.DryRun, "dry_run": p.DryRun}, nil
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
		return nil, agent.Errorf("certbot_failed", "certbot revoke: %s", failure(res, err, 6))
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
			return nil, fmt.Errorf("wp option update %s: %s", opt, failure(res, err, 3))
		}
	}
	return map[string]string{"url": p.URL}, nil
}
