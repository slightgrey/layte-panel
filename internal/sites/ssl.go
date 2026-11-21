package sites

import (
	"context"
	"errors"

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
		if err := call("ssl.issue", actions.SSLIssueParams{Domain: site.Domain, Aliases: site.Aliases, Username: site.SystemUser, Email: email}); err != nil {
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
		if err := call("ssl.revoke", actions.DomainParams{Domain: site.Domain}); err != nil {
			j.Log("Warning: revoking failed (%v). The site already serves HTTP; the certificate will simply expire.", err)
		}
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
