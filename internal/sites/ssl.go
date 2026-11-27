package sites

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// siteConfig is what the agent needs to render a managed site's vhost and
// pool. Every rewrite goes through here, so none of them drops a setting.
func (s *Service) siteConfig(site store.Site, ssl bool) actions.SiteConfigParams {
	c := actions.SiteConfigParams{Domain: site.Domain, Aliases: site.Aliases, Username: site.SystemUser, PHPVersion: site.PHPVersion,
		SSL: ssl, PHP: site.PHPSettings, Cache: site.PageCache, BlockShell: site.BlockPHPShell, OwnFPM: site.OwnFPM}
	if len(site.Redirects) > 0 {
		c.Redirects = site.Redirects // kept on every vhost write
	}
	if site.Adopted {
		c.WebRoot = site.RootPath // its files stay where they were
		c.OwnCert = ownCert(site)
	}
	return c
}

// ownCert: a taken-over adopted site may still use its own certbot
// certificate (its live folder) instead of a Layte one.
func ownCert(site store.Site) string {
	if !site.Adopted || !site.SSL {
		return ""
	}
	if dir := actions.OwnCertOf(site.CertPath); dir != templates.CertDir(site.Domain) {
		return dir
	}
	return ""
}

// layteCertPath is where the site's Layte certificate lives.
func layteCertPath(domain string) string { return templates.CertDir(domain) + "/fullchain.pem" }

// requireManagedActive: Layte renders the site's nginx config (created, or
// adopted and taken over), and no job is running.
func (s *Service) requireManagedActive(site store.Site) error {
	if !site.LayteConfigs() {
		if site.Adopted {
			return conflict("site_unmanaged", "%s's nginx config is still yours; let Layte Panel manage it first", site.Domain)
		}
		return conflict("site_unmanaged", "%s was imported from the server; Layte Panel doesn't change imported sites", site.Domain)
	}
	if site.Status != "active" {
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	}
	return nil
}

// IssueSSL starts a job that gets a Let's Encrypt certificate for the site
// (all its domains) and switches its vhost to HTTPS. The domains' DNS must
// already point to this server.
//
// For a taken-over adopted site still on its own certbot certificate, this
// switches it to a Layte one; deleteOldCert then deletes the old one.
// saveEmail keeps email as the server's default (admins only: a site
// account's address is used for its own certificate).
func (s *Service) IssueSSL(ctx context.Context, actor agent.Actor, site store.Site, email string, deleteOldCert, saveEmail bool) (string, error) {
	oldCert := ownCert(site) // "" unless an adopted site uses its own certificate
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	if err := validate.Email(email); err != nil {
		return "", badRequest("%v", err)
	}
	if saveEmail {
		if err := s.Store.SetSetting(ctx, store.SettingLetsEncryptEmail, email); err != nil {
			return "", err
		}
	}
	jobID, err := s.Jobs.Start("site.ssl_issue", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		if err := s.enableHTTPS(ctx, j, actor, &site, email); err != nil {
			return err
		}
		if deleteOldCert && oldCert != "" {
			s.deleteOldCert(ctx, j, actor, site, oldCert)
		}
		j.SetResult(map[string]any{"site_id": site.ID, "url": "https://" + site.Domain})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// enableHTTPS gets a Let's Encrypt certificate for the site's domains,
// switches its vhost to HTTPS and (WordPress sites Layte runs) the
// WordPress address to https://. It runs inside a job: the SSL tab's, or
// site creation's when "Issue SSL" was ticked.
func (s *Service) enableHTTPS(ctx context.Context, j *jobs.Job, actor agent.Actor, site *store.Site, email string) error {
	call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

	j.Step("Request certificate from Let's Encrypt")
	j.Log("Domains: %v. Let's Encrypt checks each one over HTTP, so DNS must point here.", append([]string{site.Domain}, site.Aliases...))
	if err := s.longCall(ctx, 3*time.Minute, "ssl.issue", actor, s.sslIssueParams(*site, email), nil); err != nil {
		return err
	}

	j.Step("Enable HTTPS in nginx")
	if site.Adopted {
		site.CertPath = layteCertPath(site.Domain) // replaces the site's own certificate
	}
	if err := call("nginx.vhost_write", s.siteConfig(*site, true)); err != nil {
		return err // WriteConfig already restored the previous vhost
	}
	site.SSL = true
	if err := s.Store.UpdateSite(context.WithoutCancel(ctx), *site); err != nil {
		return err
	}

	if site.WordPress && site.Adopted {
		j.Log("Change the WordPress address to https://%s under Settings → General if it isn't already.", site.Domain)
	} else if site.WordPress {
		j.Step("Switch WordPress to https://")
		if err := call("wp.set_url", actions.WPURLParams{Domain: site.Domain, Username: site.SystemUser, PHPVersion: site.PHPVersion, URL: "https://" + site.Domain}); err != nil {
			j.Log("Warning: couldn't update the WordPress URL (%v). Change it under Settings → General.", err)
		}
	}
	return nil
}

// RenewSSL starts a job that renews the site's certificate now (forced), or
// with dryRun only checks against Let's Encrypt's staging server that
// renewal works. Forced renewals count towards Let's Encrypt's limit of 5
// duplicate certificates per week.
func (s *Service) RenewSSL(ctx context.Context, actor agent.Actor, site store.Site, dryRun bool) (string, error) {
	params := actions.SSLRenewParams{Domain: site.Domain, DryRun: dryRun}
	certName := templates.CertName(site.Domain)
	if site.Adopted && !site.NginxManaged {
		// The adopted site's own certbot certificate, as its vhost names it.
		dir, file := path.Split(site.CertPath)
		if site.Status != "active" {
			return "", conflict("site_busy", "%s is %s", site.Domain, site.Status)
		}
		if file != "fullchain.pem" || !strings.HasPrefix(dir, "/etc/letsencrypt/live/") {
			return "", badRequest("%s doesn't use a certbot certificate", site.Domain)
		}
		certName = path.Base(dir)
		params.CertName, params.Username, params.WebRoot = certName, site.SystemUser, site.RootPath
	} else if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	if !site.SSL {
		return "", badRequest("%s doesn't use HTTPS", site.Domain)
	}
	kind, step := "site.ssl_renew", "Renew certificate "+certName
	if dryRun {
		kind, step = "site.ssl_renew_test", "Test renewal against Let's Encrypt staging"
	}
	jobID, err := s.Jobs.Start(kind, site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step(step)
		if err := s.longCall(ctx, 3*time.Minute, "ssl.renew", actor, params, nil); err != nil {
			return err
		}
		if dryRun {
			j.Log("Renewal works. Nothing was changed; certbot renews automatically 30 days before expiry.")
		} else {
			j.Log("New certificate installed; certbot's deploy hook reloaded nginx.")
		}
		j.SetResult(map[string]any{"site_id": site.ID, "dry_run": dryRun})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// RemoveSSL starts a job that switches the site back to HTTP and revokes its
// Layte certificate.
func (s *Service) RemoveSSL(ctx context.Context, actor agent.Actor, site store.Site) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	if !site.SSL {
		return "", badRequest("%s doesn't use HTTPS", site.Domain)
	}
	jobID, err := s.Jobs.Start("site.ssl_remove", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

		j.Step("Switch nginx to HTTP only")
		if err := call("nginx.vhost_write", s.siteConfig(site, false)); err != nil {
			return err
		}
		site.SSL = false
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		if site.WordPress && site.Adopted {
			j.Log("Change the WordPress address to http://%s under Settings → General.", site.Domain)
		} else if site.WordPress {
			j.Step("Switch WordPress to http://")
			if err := call("wp.set_url", actions.WPURLParams{Domain: site.Domain, Username: site.SystemUser, PHPVersion: site.PHPVersion, URL: "http://" + site.Domain}); err != nil {
				j.Log("Warning: couldn't update the WordPress URL (%v).", err)
			}
		}
		j.Step("Revoke certificate " + templates.CertName(site.Domain))
		if err := s.longCall(ctx, time.Minute, "ssl.revoke", actor, actions.DomainParams{Domain: site.Domain}, nil); err != nil {
			j.Log("Warning: revoking failed (%v). The site already serves HTTP; the certificate will simply expire.", err)
		}
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// sslIssueParams: adopted sites get their challenge files in their own web root.
func (s *Service) sslIssueParams(site store.Site, email string) actions.SSLIssueParams {
	p := actions.SSLIssueParams{Domain: site.Domain, Aliases: site.Aliases, Username: site.SystemUser, Email: email}
	if site.Adopted {
		p.WebRoot = site.RootPath
	}
	return p
}
