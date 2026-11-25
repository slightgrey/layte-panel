package sites

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// TakeoverPreview is the agent's preview plus the Let's Encrypt email the
// new certificate would use (sites with HTTPS).
type TakeoverPreview struct {
	actions.TakeoverPreview
	Email string `json:"email"`
}

func takeoverable(site store.Site) error {
	switch {
	case !site.Adopted:
		return conflict("site_not_adopted", "only adopted sites can be taken over; adopt %s first", site.Domain)
	case site.NginxManaged:
		return conflict("site_managed", "Layte Panel already manages the nginx config of %s", site.Domain)
	case site.Status != "active":
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	}
	return nil
}

func takeoverParams(site store.Site) actions.TakeoverParams {
	return actions.TakeoverParams{Domain: site.Domain, Username: site.SystemUser, WebRoot: site.RootPath}
}

// TakeoverPreview shows what letting Layte manage the adopted site's nginx
// config would change. Nothing is changed.
func (s *Service) TakeoverPreview(ctx context.Context, actor agent.Actor, site store.Site) (TakeoverPreview, error) {
	if err := takeoverable(site); err != nil {
		return TakeoverPreview{}, err
	}
	var p actions.TakeoverPreview
	if err := s.Agent.Call(ctx, "site.takeover_preview", actor, takeoverParams(site), &p); err != nil {
		return TakeoverPreview{}, err
	}
	email, _ := s.Store.Setting(ctx, store.SettingLetsEncryptEmail)
	return TakeoverPreview{TakeoverPreview: p, Email: email}, nil
}

// Takeover starts a job that lets Layte manage an adopted site's nginx
// config and PHP pool (files stay in their web root). A site with HTTPS
// keeps its own certificate at first, then gets a Layte one through Layte's
// config (which always serves Let's Encrypt's challenge files, unlike many
// hand-written configs). If that fails, the site keeps its own certificate.
// A site with HTTPS but no certbot certificate gets the Layte certificate
// first. vhostHash comes from the preview the admin confirmed.
//
// deleteOldCert: once HTTPS uses the Layte certificate, delete the site's
// old certbot certificate (see deleteOldCert).
func (s *Service) Takeover(ctx context.Context, actor agent.Actor, site store.Site, vhostHash, email string, deleteOldCert bool) (string, error) {
	if err := takeoverable(site); err != nil {
		return "", err
	}
	params := takeoverParams(site)
	params.VhostHash, params.SSL = vhostHash, site.SSL
	if site.SSL {
		params.OwnCert = actions.OwnCertOf(site.CertPath)
	}
	if err := params.Validate(); err != nil {
		return "", badRequest("%v", err)
	}
	if vhostHash == "" {
		return "", badRequest("vhost_hash from the preview is required")
	}
	if site.SSL {
		if err := validate.Email(email); err != nil {
			return "", badRequest("%v", err)
		}
		if err := s.Store.SetSetting(ctx, store.SettingLetsEncryptEmail, email); err != nil {
			return "", err
		}
	}
	issue := func(ctx context.Context, j *jobs.Job) error {
		j.Log("certbot puts a challenge file in %s, which nginx serves.", site.RootPath)
		return s.longCall(ctx, 3*time.Minute, "ssl.issue", actor, s.sslIssueParams(site, email), nil)
	}
	jobID, err := s.Jobs.Start("site.nginx_takeover", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		if site.SSL && params.OwnCert == "" {
			j.Step("Get a Layte certificate from Let's Encrypt")
			if err := issue(ctx, j); err != nil {
				return err
			}
		}
		j.Step("Let Layte Panel manage the nginx config of " + site.Domain)
		j.Log("Every step is undone if a later one fails or the site answers differently afterwards.")
		if params.OwnCert != "" {
			j.Log("HTTPS keeps using your certificate in %s for now.", params.OwnCert)
		}
		var res actions.TakeoverResult
		if err := s.longCall(ctx, 5*time.Minute, "site.nginx_takeover", actor, params, &res); err != nil {
			if site.SSL && params.OwnCert == "" {
				j.Log("The new certificate %s stays; it is unused until you try again.", templates.CertName(site.Domain))
			}
			return err
		}
		for _, step := range res.Steps {
			j.Log("%s", step)
		}
		j.Step("Save site")
		site.NginxManaged, site.NginxConf, site.FPMPool, site.PHPVersion = true, res.VhostFile, res.PoolFile, res.PHPVersion
		site.PHPSettings = phpini.Settings{} // Layte's defaults, as in the new pool
		if site.SSL && params.OwnCert == "" {
			site.CertPath = layteCertPath(site.Domain)
		}
		bg := context.WithoutCancel(ctx)
		if err := s.Store.UpdateSite(bg, site); err != nil {
			return err
		}
		if res.KeptAs != "" {
			j.Log("Your original nginx config is kept as %s.", res.KeptAs)
		}
		j.SetResult(map[string]any{"site_id": site.ID, "vhost": res.VhostFile, "kept_as": res.KeptAs})

		if params.OwnCert == "" {
			return nil
		}
		// Taken over. Switching to a Layte certificate is best effort: if it
		// fails, the site keeps serving HTTPS with its own certificate.
		j.Step("Switch to a Layte certificate")
		keep := func(err error) error {
			j.Log("Warning: %v", err)
			j.Log("%s keeps using its own certificate (%s). Try again on the SSL tab: Switch to a Layte certificate.", site.Domain, params.OwnCert)
			return nil
		}
		if err := issue(ctx, j); err != nil {
			return keep(err)
		}
		next := site
		next.CertPath = layteCertPath(site.Domain)
		if err := s.Agent.Call(ctx, "nginx.vhost_write", actor, s.siteConfig(next, true), nil); err != nil {
			return keep(err)
		}
		if err := s.Store.UpdateSite(bg, next); err != nil {
			return err
		}
		j.Log("HTTPS now uses %s, which Layte renews.", templates.CertName(site.Domain))
		if deleteOldCert {
			s.deleteOldCert(ctx, j, actor, site, params.OwnCert)
		} else {
			j.Log("Your old certificate %s is no longer used; certbot still renews it until you delete it.", path.Base(params.OwnCert))
		}
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// deleteOldCert deletes the site's previous certbot certificate (live folder
// dir) once its Layte certificate replaced it. Best effort: the agent keeps
// the certificate if nginx still uses it or it isn't for this site, and the
// job only logs why.
func (s *Service) deleteOldCert(ctx context.Context, j *jobs.Job, actor agent.Actor, site store.Site, dir string) {
	name := path.Base(dir)
	j.Step("Delete the old certificate " + name)
	err := s.longCall(ctx, time.Minute, "ssl.delete_unused", actor, actions.SSLDeleteParams{Domain: site.Domain, CertName: name}, nil)
	if err != nil {
		j.Log("Warning: kept %s (%v).", name, err)
		return
	}
	j.Log("Deleted %s; certbot no longer renews it.", name)
}
