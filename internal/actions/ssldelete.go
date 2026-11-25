package actions

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
)

// Deleting a site's old certbot certificate once Layte's own certificate
// replaced it (after taking over an adopted site). Left alone, certbot would
// keep renewing it, and its nginx installer could then write it back into
// Layte's vhost.

// SSLDeleteParams name the old certificate and the site it was for.
type SSLDeleteParams struct {
	Domain   string `json:"domain"`
	CertName string `json:"cert_name"`
}

func (p SSLDeleteParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if !certNameRe.MatchString(p.CertName) || strings.Contains(p.CertName, "..") {
		return fmt.Errorf("invalid certificate name %q", p.CertName)
	}
	if strings.HasPrefix(p.CertName, "layte-") {
		return fmt.Errorf("%s is a Layte certificate; remove HTTPS on the SSL tab instead", p.CertName)
	}
	return nil
}

// sslDeleteUnused deletes the certificate with certbot, only if it covers
// the site's domain and nothing in nginx's effective config (nginx -T)
// uses it any more.
func (d Deps) sslDeleteUnused(ctx context.Context, _ agent.Actor, p SSLDeleteParams) (any, error) {
	live := letsencryptLive + "/" + p.CertName
	b, err := d.FS.ReadFile(live + "/cert.pem")
	if err != nil {
		return nil, agent.Errorf(agent.CodeNotFound, "no certificate %s", p.CertName)
	}
	cert, err := parseCert(b)
	if err != nil {
		return nil, fmt.Errorf("read certificate %s: %w", p.CertName, err)
	}
	if !slices.Contains(cert.DNSNames, p.Domain) {
		return nil, agent.Errorf(agent.CodeInvalidParams, "certificate %s isn't for %s", p.CertName, p.Domain)
	}
	res, err := d.Runner.Run(ctx, nginxBin, "-T")
	if err != nil {
		return nil, agent.Errorf("nginx_failed", "can't check where %s is used: %s", p.CertName, failure(res, err, 3))
	}
	if strings.Contains(string(res.Stdout), live+"/") {
		return nil, agent.Errorf(agent.CodeInvalidParams, "nginx still uses certificate %s; it was kept", p.CertName)
	}
	certbot, err := d.certbotBin()
	if err != nil {
		return nil, err
	}
	if res, err := d.Runner.Run(ctx, certbot, "delete", "--cert-name", p.CertName, "--non-interactive"); err != nil {
		return nil, agent.Errorf("certbot_failed", "certbot delete: %s", failure(res, err, 6))
	}
	return map[string]bool{"deleted": true}, nil
}
