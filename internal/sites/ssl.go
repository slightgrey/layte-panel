package sites

import (
	"context"
	"errors"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

func (s *Service) siteConfig(site store.Site, ssl bool) actions.SiteConfigParams {
	return actions.SiteConfigParams{Domain: site.Domain, Aliases: site.Aliases, Username: site.SystemUser, PHPVersion: site.PHPVersion, SSL: ssl}
}

func (s *Service) requireManagedActive(site store.Site) error {
	if !site.Managed {
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
func (s *Service) IssueSSL(ctx context.Context, actor agent.Actor, site store.Site, email string) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	if err := validate.Email(email); err != nil {
		return "", badRequest("%v", err)
	}
	if err := s.Store.SetSetting(ctx, store.SettingLetsEncryptEmail, email); err != nil {
		return "", err
	}
	jobID, err := s.Jobs.Start("site.ssl_issue", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

		j.Step("Request certificate from Let's Encrypt")
		j.Log("Domains: %v. Let's Encrypt checks each one over HTTP, so DNS must point here.", append([]string{site.Domain}, site.Aliases...))
		if err := s.longCall(ctx, 3*time.Minute, "ssl.issue", actor, actions.SSLIssueParams{Domain: site.Domain, Aliases: site.Aliases, Username: site.SystemUser, Email: email}, nil); err != nil {
			return err
		}

		j.Step("Enable HTTPS in nginx")
		if err := call("nginx.vhost_write", s.siteConfig(site, true)); err != nil {
			return err // WriteConfig already restored the HTTP-only vhost
		}
		site.SSL = true
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}

		if site.WordPress {
			j.Step("Switch WordPress to https://")
			if err := call("wp.set_url", actions.WPURLParams{Domain: site.Domain, Username: site.SystemUser, PHPVersion: site.PHPVersion, URL: "https://" + site.Domain}); err != nil {
				j.Log("Warning: couldn't update the WordPress URL (%v). Change it under Settings → General.", err)
			}
		}
		j.SetResult(map[string]any{"site_id": site.ID, "url": "https://" + site.Domain})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// RenewSSL starts a job that renews the site's certificate now (forced), or
// with dryRun only checks against Let's Encrypt's staging server that
// renewal works. Forced renewals count towards Let's Encrypt's limit of 5
// duplicate certificates per week.
func (s *Service) RenewSSL(ctx context.Context, actor agent.Actor, site store.Site, dryRun bool) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	if !site.SSL {
		return "", badRequest("%s doesn't use HTTPS", site.Domain)
	}
	kind, step := "site.ssl_renew", "Renew certificate "+templates.CertName(site.Domain)
	if dryRun {
		kind, step = "site.ssl_renew_test", "Test renewal against Let's Encrypt staging"
	}
	jobID, err := s.Jobs.Start(kind, site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step(step)
		if err := s.longCall(ctx, 3*time.Minute, "ssl.renew", actor, actions.SSLRenewParams{Domain: site.Domain, DryRun: dryRun}, nil); err != nil {
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
		if site.WordPress {
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
