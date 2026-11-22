package sites

import (
	"context"
	"errors"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// AdoptPreview is the agent's preview plus the suggested new username.
type AdoptPreview struct {
	actions.AdoptPreview
	Username string `json:"username"`
}

func adoptable(site store.Site) error {
	switch {
	case site.Managed:
		return conflict("site_managed", "%s was created by Layte Panel already", site.Domain)
	case site.Adopted:
		return conflict("site_adopted", "%s is already adopted", site.Domain)
	case site.Status != "active":
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	case site.RootPath == "":
		return badRequest("%s has no web root in its nginx config", site.Domain)
	}
	return nil
}

// AdoptPreview shows what adopting the imported site would change. Nothing
// is changed.
func (s *Service) AdoptPreview(ctx context.Context, actor agent.Actor, site store.Site) (AdoptPreview, error) {
	if err := adoptable(site); err != nil {
		return AdoptPreview{}, err
	}
	var p actions.AdoptPreview
	if err := s.Agent.Call(ctx, "site.adopt_preview", actor, actions.AdoptPreviewParams{Domain: site.Domain, WebRoot: site.RootPath}, &p); err != nil {
		return AdoptPreview{}, err
	}
	username, err := s.freeUsername(ctx, validate.DefaultUsername(site.Domain))
	if err != nil {
		return AdoptPreview{}, err
	}
	return AdoptPreview{AdoptPreview: p, Username: username}, nil
}

// Adopt starts a job that adopts an imported site: a new Linux user, its
// files owned by that user, and its PHP-FPM pool running as it. The nginx
// vhost isn't touched. poolHash comes from the preview the admin confirmed.
func (s *Service) Adopt(ctx context.Context, actor agent.Actor, site store.Site, username, poolHash string) (string, error) {
	if err := adoptable(site); err != nil {
		return "", err
	}
	if err := validate.SiteUsername(username); err != nil {
		return "", badRequest("%v", err)
	}
	if inUse, err := s.Store.SystemUserInUse(ctx, username); err != nil {
		return "", err
	} else if inUse {
		return "", conflict("username_taken", "username %s is already used by another site", username)
	}
	params := actions.AdoptParams{Domain: site.Domain, WebRoot: site.RootPath, Username: username, PoolHash: poolHash}
	if err := params.Validate(); err != nil {
		return "", badRequest("%v", err)
	}
	jobID, err := s.Jobs.Start("site.adopt", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step("Adopt " + site.Domain + " as " + username)
		j.Log("Every step is undone if a later one fails or the site answers differently afterwards.")
		var res actions.AdoptResult
		if err := s.longCall(ctx, 5*time.Minute, "site.adopt", actor, params, &res); err != nil {
			return err
		}
		for _, step := range res.Steps {
			j.Log("%s", step)
		}
		j.Step("Save site")
		site.Adopted, site.SystemUser, site.FPMPool, site.PHPVersion = true, res.Username, res.PoolFile, res.PHPVersion
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		j.Log("The original pool file is kept as %s.layte-orig.", res.PoolFile)
		j.SetResult(map[string]any{"site_id": site.ID, "username": res.Username, "old_user": res.OldUser})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
