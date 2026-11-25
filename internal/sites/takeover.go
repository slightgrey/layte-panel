package sites

import (
	"context"
	"errors"
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
// first gets a Layte certificate; if that fails, nothing has changed.
// vhostHash comes from the preview the admin confirmed.
func (s *Service) Takeover(ctx context.Context, actor agent.Actor, site store.Site, vhostHash, email string) (string, error) {
	if err := takeoverable(site); err != nil {
		return "", err
	}
	params := takeoverParams(site)
	params.VhostHash, params.SSL = vhostHash, site.SSL
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
	jobID, err := s.Jobs.Start("site.nginx_takeover", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		if site.SSL {
			j.Step("Get a Layte certificate from Let's Encrypt")
			j.Log("certbot puts a challenge file in %s, which your current nginx config serves.", site.RootPath)
			if err := s.longCall(ctx, 3*time.Minute, "ssl.issue", actor, s.sslIssueParams(site, email), nil); err != nil {
				return err
			}
		}
		j.Step("Let Layte Panel manage the nginx config of " + site.Domain)
		j.Log("Every step is undone if a later one fails or the site answers differently afterwards.")
		var res actions.TakeoverResult
		if err := s.longCall(ctx, 5*time.Minute, "site.nginx_takeover", actor, params, &res); err != nil {
			if site.SSL {
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
		if site.SSL {
			site.CertPath = templates.CertDir(site.Domain) + "/fullchain.pem"
		}
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		if res.KeptAs != "" {
			j.Log("Your original nginx config is kept as %s.", res.KeptAs)
		}
		j.SetResult(map[string]any{"site_id": site.ID, "vhost": res.VhostFile, "kept_as": res.KeptAs})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
