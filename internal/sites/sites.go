// Package sites orchestrates multi-step site operations (create, delete,
// switch PHP) in layte serve. Each step is one small agent action; if a step
// fails, the steps already done are undone in reverse order. The agent stays
// a strict executor and never needs to know about the overall flow.
package sites

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"slices"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Caller is the part of agent.Client used here.
type Caller interface {
	Call(ctx context.Context, action string, actor agent.Actor, params, out any) error
}

type Service struct {
	Store *store.Store
	Agent Caller
	Jobs  *jobs.Runner
}

// Error is a request problem the API shows to the user (400/409).
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func badRequest(format string, args ...any) *Error {
	return &Error{Status: 400, Code: "invalid", Message: fmt.Sprintf(format, args...)}
}

func conflict(code, format string, args ...any) *Error {
	return &Error{Status: 409, Code: code, Message: fmt.Sprintf(format, args...)}
}

// CreateRequest is the "New site" form.
type CreateRequest struct {
	Domain     string           `json:"domain"`
	Aliases    []string         `json:"aliases"`
	Username   string           `json:"username"` // optional; derived from the domain
	PHPVersion string           `json:"php_version"`
	CreateDB   bool             `json:"create_db"`
	WordPress  *WordPressFields `json:"wordpress"`
}

type WordPressFields struct {
	Title      string `json:"title"`
	AdminUser  string `json:"admin_user"`
	AdminEmail string `json:"admin_email"`
}

// Create validates the request, records the site as "creating" and starts the
// creation job. It returns the job and site IDs.
func (s *Service) Create(ctx context.Context, actor agent.Actor, req CreateRequest) (jobID string, siteID int64, err error) {
	domain, err := validate.Domain(req.Domain)
	if err != nil {
		return "", 0, badRequest("%v", err)
	}
	aliases, err := validate.Domains(domain, req.Aliases)
	if err != nil {
		return "", 0, badRequest("%v", err)
	}
	var installed []string
	if err := s.Agent.Call(ctx, "php.versions", actor, nil, &installed); err != nil {
		return "", 0, err
	}
	if err := validate.PHPVersion(req.PHPVersion, installed); err != nil {
		return "", 0, badRequest("%v", err)
	}
	if req.WordPress != nil {
		req.CreateDB = true // WordPress needs a database
		wp := req.WordPress
		if err := validate.Title(wp.Title); err != nil {
			return "", 0, badRequest("site title: %v", err)
		}
		if err := validate.Username(wp.AdminUser); err != nil {
			return "", 0, badRequest("WordPress admin username: %v", err)
		}
		if err := validate.Email(wp.AdminEmail); err != nil {
			return "", 0, badRequest("%v", err)
		}
	}

	// Conflicts with existing sites (managed or imported).
	existing, err := s.Store.ListSites(ctx)
	if err != nil {
		return "", 0, err
	}
	taken := map[string]string{}
	for _, x := range existing {
		taken[x.Domain] = x.Domain
		for _, a := range x.Aliases {
			taken[a] = x.Domain
		}
	}
	for _, d := range append([]string{domain}, aliases...) {
		if owner, ok := taken[d]; ok {
			return "", 0, conflict("domain_taken", "%s is already configured on this server (site %s)", d, owner)
		}
	}

	username := req.Username
	if username == "" {
		if username, err = s.freeUsername(ctx, validate.DefaultUsername(domain)); err != nil {
			return "", 0, err
		}
	} else if inUse, err := s.Store.SystemUserInUse(ctx, username); err != nil {
		return "", 0, err
	} else if inUse {
		return "", 0, conflict("username_taken", "username %s is already used by another site", username)
	}
	if err := validate.SiteUsername(username); err != nil {
		return "", 0, badRequest("%v", err)
	}

	tsite := templates.Site{Domain: domain, Aliases: aliases, User: username, PHPVersion: req.PHPVersion}
	site := store.Site{
		Domain: domain, Aliases: aliases, SystemUser: username, RootPath: tsite.Dir() + "/public",
		PHPVersion: req.PHPVersion, Managed: true, Source: "created", WordPress: req.WordPress != nil,
		Status: "creating", NginxConf: templates.VhostPath(domain), FPMPool: templates.PoolPath(req.PHPVersion, domain),
	}
	if req.CreateDB {
		site.DBName = dbIdent(username)
		site.DBUser = site.DBName
	}
	siteID, err = s.Store.CreateSite(ctx, site)
	if errors.Is(err, store.ErrExists) {
		return "", 0, conflict("domain_taken", "%s already exists", domain)
	}
	if err != nil {
		return "", 0, err
	}
	site.ID = siteID

	jobID, err = s.Jobs.Start("site.create", siteID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		return s.runCreate(ctx, j, actor, site, aliases, req.WordPress)
	})
	if err != nil {
		s.Store.DeleteSite(ctx, siteID)
		return "", 0, err
	}
	return jobID, siteID, nil
}

// undoStack collects compensating steps; run executes them newest first.
type undoStack []struct {
	name string
	fn   func(context.Context) error
}

func (u *undoStack) push(name string, fn func(context.Context) error) {
	*u = append(*u, struct {
		name string
		fn   func(context.Context) error
	}{name, fn})
}

// run undoes everything; it keeps going after failures and reports whether
// all undo steps succeeded.
func (u undoStack) run(j *jobs.Job) bool {
	// Undo must finish even if the job's context was cancelled (shutdown).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ok := true
	for i := len(u) - 1; i >= 0; i-- {
		if err := u[i].fn(ctx); err != nil {
			j.Log("Rollback: %s failed: %v", u[i].name, err)
			ok = false
		} else {
			j.Log("Rollback: %s", u[i].name)
		}
	}
	return ok
}

func (s *Service) runCreate(ctx context.Context, j *jobs.Job, actor agent.Actor, site store.Site, aliases []string, wp *WordPressFields) (err error) {
	var undo undoStack
	call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }
	cfg := s.siteConfig(site, false) // HTTPS comes later, in its own step
	cfg.Aliases = aliases

	defer func() {
		if err == nil {
			return
		}
		j.FailStep()
		j.Step("Rolling back")
		if undo.run(j) {
			s.Store.DeleteSite(context.WithoutCancel(ctx), site.ID)
			err = fmt.Errorf("%w (all changes were rolled back)", err)
		} else {
			s.Store.SetSiteStatus(context.WithoutCancel(ctx), site.ID, "failed")
			err = fmt.Errorf("%w (rollback incomplete: delete the site to clean up)", err)
		}
	}()

	j.Step("Create Linux user " + site.SystemUser)
	var created struct {
		Created bool `json:"created"`
	}
	if err := s.Agent.Call(ctx, "site.user_create", actor, actions.UserParams{Username: site.SystemUser}, &created); err != nil {
		return err
	}
	if created.Created {
		undo.push("remove Linux user", func(ctx context.Context) error {
			return s.Agent.Call(ctx, "site.user_remove", actor, actions.UserParams{Username: site.SystemUser}, nil)
		})
	}

	j.Step("Create site directories")
	if err := call("site.dirs_create", actions.DirsParams{Username: site.SystemUser, Domain: site.Domain}); err != nil {
		return err
	}

	j.Step("Configure PHP " + site.PHPVersion + " (FPM pool)")
	if err := call("php.pool_write", cfg); err != nil {
		return err
	}
	undo.push("remove PHP-FPM pool", func(ctx context.Context) error {
		return s.Agent.Call(ctx, "php.pool_remove", actor, actions.PoolRemoveParams{Domain: site.Domain, PHPVersion: site.PHPVersion}, nil)
	})

	j.Step("Configure nginx")
	var vhost struct {
		Path string `json:"path"`
	}
	if err := s.Agent.Call(ctx, "nginx.vhost_write", actor, cfg, &vhost); err != nil {
		return err
	}
	if vhost.Path != "" {
		site.NginxConf = vhost.Path // the agent picks sites-available or conf.d
	}
	undo.push("remove nginx config", func(ctx context.Context) error {
		return s.Agent.Call(ctx, "nginx.vhost_remove", actor, actions.DomainParams{Domain: site.Domain}, nil)
	})

	secrets := map[string]string{}
	if site.DBName != "" {
		j.Step("Create database " + site.DBName)
		pw := randomPassword()
		if err := call("db.create", actions.DBCreateParams{Name: site.DBName, User: site.DBUser, Password: pw}); err != nil {
			return err
		}
		undo.push("drop database", func(ctx context.Context) error {
			s.Store.RemoveDatabase(ctx, site.DBName)
			return s.Agent.Call(ctx, "db.drop", actor, actions.DBDropParams{Name: site.DBName, User: site.DBUser}, nil)
		})
		if err := s.Store.AddDatabase(context.WithoutCancel(ctx), store.Database{Name: site.DBName, User: site.DBUser, SiteID: site.ID}); err != nil {
			return err
		}
		secrets["db_name"], secrets["db_user"], secrets["db_password"] = site.DBName, site.DBUser, pw
	}

	if wp != nil {
		j.Step("Install WordPress")
		j.Log("Downloading WordPress and running the installer as %s…", site.SystemUser)
		adminPW := randomPassword()
		err := s.longCall(ctx, 5*time.Minute, "wp.install", actor, actions.WPInstallParams{
			Domain: site.Domain, Username: site.SystemUser, PHPVersion: site.PHPVersion,
			URL: "http://" + site.Domain, Title: wp.Title, AdminUser: wp.AdminUser, AdminEmail: wp.AdminEmail,
			AdminPassword: adminPW, DBName: site.DBName, DBUser: site.DBUser, DBPassword: secrets["db_password"],
		}, nil)
		if err != nil {
			return err
		}
		secrets["wp_admin_url"] = "http://" + site.Domain + "/wp-admin/"
		secrets["wp_admin_user"], secrets["wp_admin_password"] = wp.AdminUser, adminPW
	}

	site.Status = "active"
	if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
		return err
	}
	j.Log("Site is live at http://%s (point its DNS to this server; HTTPS comes with the SSL feature)", site.Domain)
	j.SetResult(map[string]any{"site_id": site.ID, "domain": site.Domain, "url": "http://" + site.Domain})
	if len(secrets) > 0 {
		j.SetSecret(secrets)
	}
	return nil
}

// DeleteRequest confirms a site deletion.
type DeleteRequest struct {
	ConfirmDomain string `json:"confirm_domain"`
	RemoveFiles   bool   `json:"remove_files"` // delete the Linux user + home (all site files)
	DropDB        bool   `json:"drop_db"`
}

// Delete starts a job that removes a managed site.
func (s *Service) Delete(ctx context.Context, actor agent.Actor, site store.Site, req DeleteRequest) (string, error) {
	if !site.Managed {
		return "", conflict("site_unmanaged", "%s was imported from the server; Layte Panel doesn't change imported sites", site.Domain)
	}
	if strings.TrimSpace(strings.ToLower(req.ConfirmDomain)) != site.Domain {
		return "", badRequest("type the domain (%s) to confirm", site.Domain)
	}
	if site.Status == "creating" || site.Status == "deleting" {
		return "", conflict("site_busy", "%s is busy (%s)", site.Domain, site.Status)
	}
	jobID, err := s.Jobs.Start("site.delete", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		s.Store.SetSiteStatus(ctx, site.ID, "deleting")
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }
		fail := func(err error) error {
			s.Store.SetSiteStatus(context.WithoutCancel(ctx), site.ID, "failed")
			return err
		}
		if site.ObjectCache.Enabled {
			j.Step("Stop the object cache")
			if p, err := wpParams(site, "the object cache"); err == nil {
				if err := s.longCall(ctx, 2*time.Minute, "object_cache.disable", actor, actions.ObjectCacheParams{WPLoginParams: p}, nil); err != nil {
					j.Log("Warning: couldn't stop the object cache (%v).", err)
				}
			}
		}
		if site.SFTP.Enabled {
			j.Step("Turn off SFTP")
			if err := s.longCall(ctx, time.Minute, "sftp.disable", actor, sftpParams(site), nil); err != nil {
				return fail(err)
			}
		}
		j.Step("Remove nginx config")
		if err := call("nginx.vhost_remove", actions.DomainParams{Domain: site.Domain}); err != nil {
			return fail(err)
		}
		if site.PageCache.Enabled {
			j.Step("Remove cached pages")
			if err := call("cache.purge", actions.CacheParams{Domain: site.Domain, Username: site.SystemUser, Remove: true}); err != nil {
				j.Log("Warning: couldn't delete the cached pages in %s (%v).", pagecache.Dir(site.Domain), err)
			}
		}
		j.Step("Remove cron jobs")
		if err := call("cron.write", actions.CronWriteParams{Domain: site.Domain, Username: site.SystemUser}); err != nil {
			return fail(err)
		}
		if site.SSL {
			j.Step("Revoke certificate")
			if err := s.longCall(ctx, time.Minute, "ssl.revoke", actor, actions.DomainParams{Domain: site.Domain}, nil); err != nil {
				j.Log("Warning: revoking failed (%v); the certificate will simply expire.", err)
			}
		}
		j.Step("Remove PHP-FPM pool")
		if site.PHPVersion != "" {
			if err := call("php.pool_remove", actions.PoolRemoveParams{Domain: site.Domain, PHPVersion: site.PHPVersion}); err != nil {
				return fail(err)
			}
		}
		if req.DropDB {
			dbs, err := s.siteDatabases(ctx, site)
			if err != nil {
				return fail(err)
			}
			for _, db := range dbs {
				j.Step("Drop database " + db.Name)
				if err := call("db.drop", actions.DBDropParams{Name: db.Name, User: db.User}); err != nil {
					return fail(err)
				}
				s.Store.RemoveDatabase(context.WithoutCancel(ctx), db.Name)
			}
		}
		// Without DropDB, linked databases are kept: deleting the site row unlinks
		// them (ON DELETE SET NULL) and they stay manageable on the Databases page.
		if req.RemoveFiles {
			j.Step("Remove Linux user " + site.SystemUser + " and all site files")
			if err := s.longCall(ctx, time.Minute, "site.user_remove", actor, actions.UserParams{Username: site.SystemUser}, nil); err != nil {
				return fail(err)
			}
		} else {
			j.Log("Kept %s and its files (user %s).", templates.Site{Domain: site.Domain, User: site.SystemUser}.Dir(), site.SystemUser)
		}
		if err := s.Store.DeleteSite(context.WithoutCancel(ctx), site.ID); err != nil {
			return err
		}
		j.SetResult(map[string]any{"domain": site.Domain, "deleted": true})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// siteDatabases returns every database linked to the site: the ones in the
// databases table plus site.DBName (sites created before that table existed).
func (s *Service) siteDatabases(ctx context.Context, site store.Site) ([]store.Database, error) {
	dbs, err := s.Store.DatabasesForSite(ctx, site.ID)
	if err != nil {
		return nil, fmt.Errorf("list databases of %s: %w", site.Domain, err)
	}
	if site.DBName != "" && !slices.ContainsFunc(dbs, func(d store.Database) bool { return d.Name == site.DBName }) {
		dbs = append(dbs, store.Database{Name: site.DBName, User: site.DBUser, SiteID: site.ID})
	}
	return dbs, nil
}

// longCall calls an agent action that may run longer than the client's
// default 60 s (agent.DefaultCallTimeout). timeout is the action's own limit
// in the agent's registry; a margin is added so the agent, which enforces
// that limit, always answers first and its error ("action timed out") is
// what the job shows.
func (s *Service) longCall(ctx context.Context, timeout time.Duration, action string, actor agent.Actor, params, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()
	return s.Agent.Call(ctx, action, actor, params, out)
}

// freeUsername returns base, or base2, base3, … if taken by another site.
func (s *Service) freeUsername(ctx context.Context, base string) (string, error) {
	for i := 1; i < 100; i++ {
		u := base
		if i > 1 {
			suffix := fmt.Sprint(i)
			if len(u)+len(suffix) > 32 {
				u = u[:32-len(suffix)]
			}
			u += suffix
		}
		inUse, err := s.Store.SystemUserInUse(ctx, u)
		if err != nil {
			return "", err
		}
		if !inUse {
			return u, nil
		}
	}
	return "", conflict("username_taken", "no free username for %s", base)
}

// dbIdent turns a site username into a database name (no dashes).
func dbIdent(username string) string {
	n := strings.ReplaceAll(username, "-", "_")
	if len(n) > 32 {
		n = n[:32]
	}
	return n
}

// randomPassword returns 32 characters from crypto/rand (base32: A-Z, 2-7;
// 160 bits). rand.Text (Go 1.24+) can't fail.
func randomPassword() string {
	return (rand.Text() + rand.Text())[:32]
}
