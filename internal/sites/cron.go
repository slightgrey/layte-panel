package sites

import (
	"context"
	"errors"
	"strings"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// CronView is a cron job with its site's domain.
type CronView struct {
	store.CronJob
	SiteDomain string `json:"site_domain"`
}

// ListCron returns every cron job, plus a suggested WP-Cron command per
// WordPress site (running WP-Cron from real cron is faster than on page loads).
func (s *Service) ListCron(ctx context.Context) ([]CronView, map[int64]string, error) {
	jobs, err := s.Store.ListCronJobs(ctx, 0)
	if err != nil {
		return nil, nil, err
	}
	sites, err := s.Store.ListSites(ctx)
	if err != nil {
		return nil, nil, err
	}
	domains := map[int64]string{}
	suggest := map[int64]string{}
	for _, x := range sites {
		domains[x.ID] = x.Domain
		if x.Managed && x.WordPress {
			suggest[x.ID] = actions.WPCronCommand(templates.Site{Domain: x.Domain, User: x.SystemUser, PHPVersion: x.PHPVersion})
		}
	}
	out := make([]CronView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, CronView{CronJob: j, SiteDomain: domains[j.SiteID]})
	}
	return out, suggest, nil
}

// AddCron adds a job to a managed site and re-installs its cron file.
func (s *Service) AddCron(ctx context.Context, actor agent.Actor, site store.Site, schedule, command string) (int64, error) {
	if !site.Managed {
		return 0, conflict("site_unmanaged", "cron jobs can only be added to sites created by Layte Panel")
	}
	schedule = strings.Join(strings.Fields(schedule), " ")
	if err := validate.CronSchedule(schedule); err != nil {
		return 0, badRequest("%v", err)
	}
	if err := validate.CronCommand(command); err != nil {
		return 0, badRequest("%v", err)
	}
	id, err := s.Store.AddCronJob(ctx, store.CronJob{SiteID: site.ID, Schedule: schedule, Command: command, Enabled: true})
	if err != nil {
		return 0, err
	}
	if err := s.syncCron(ctx, actor, site); err != nil {
		s.Store.DeleteCronJob(ctx, id)
		return 0, err
	}
	return id, nil
}

// SetCronEnabled pauses or resumes a job.
func (s *Service) SetCronEnabled(ctx context.Context, actor agent.Actor, id int64, enabled bool) error {
	job, site, err := s.cronJob(ctx, id)
	if err != nil {
		return err
	}
	if err := s.Store.SetCronEnabled(ctx, id, enabled); err != nil {
		return err
	}
	if err := s.syncCron(ctx, actor, site); err != nil {
		s.Store.SetCronEnabled(ctx, id, job.Enabled)
		return err
	}
	return nil
}

// DeleteCron removes a job.
func (s *Service) DeleteCron(ctx context.Context, actor agent.Actor, id int64) error {
	_, site, err := s.cronJob(ctx, id)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteCronJob(ctx, id); err != nil {
		return err
	}
	return s.syncCron(ctx, actor, site)
}

func (s *Service) cronJob(ctx context.Context, id int64) (store.CronJob, store.Site, error) {
	job, err := s.Store.CronJobByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return job, store.Site{}, &Error{Status: 404, Code: "not_found", Message: "no such cron job"}
	}
	if err != nil {
		return job, store.Site{}, err
	}
	site, err := s.Store.SiteByID(ctx, job.SiteID)
	return job, site, err
}

// syncCron writes the site's enabled jobs to its cron.d file.
func (s *Service) syncCron(ctx context.Context, actor agent.Actor, site store.Site) error {
	jobs, err := s.Store.ListCronJobs(ctx, site.ID)
	if err != nil {
		return err
	}
	entries := []actions.CronEntry{}
	for _, j := range jobs {
		if j.Enabled {
			entries = append(entries, actions.CronEntry{Schedule: j.Schedule, Command: j.Command})
		}
	}
	return s.Agent.Call(ctx, "cron.write", actor, actions.CronWriteParams{Domain: site.Domain, Username: site.SystemUser, Jobs: entries}, nil)
}
