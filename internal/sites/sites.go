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
	// ParentSiteID makes a subdomain site of that main site: the domain must
	// be <label>.<main domain>, and it shares the main site's Linux user (no
	// aliases, no username of its own).
	ParentSiteID int64 `json:"parent_site_id"`
	// SSL, when set, issues a Let's Encrypt certificate once the site is
	// live (the domains' DNS must already point here). If that fails, the
	// site stays up on HTTP.
	SSL *SSLFields `json:"ssl"`
	// SaveSSLEmail keeps the SSL email as the server's default (admins).
	SaveSSLEmail bool `json:"-"`
	// ReservedHosts are names no site may take: the panel's own address.
	ReservedHosts []string `json:"-"`
}

type SSLFields struct {
	Email string `json:"email"`
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

	if req.SSL != nil {
		if err := validate.Email(req.SSL.Email); err != nil {
			return "", 0, badRequest("SSL: %v", err)
		}
		if req.SaveSSLEmail {
			if err := s.Store.SetSetting(ctx, store.SettingLetsEncryptEmail, req.SSL.Email); err != nil {
				return "", 0, err
			}
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
	if err := s.checkNginxNames(ctx, actor, append([]string{domain}, aliases...), req.ReservedHosts); err != nil {
		return "", 0, err
	}

	username := req.Username
	var parent store.Site
	switch {
	case req.ParentSiteID != 0:
		if parent, err = s.subdomainParent(ctx, req.ParentSiteID, domain, aliases); err != nil {
			return "", 0, err
		}
		username = parent.SystemUser
	case username == "":
		if username, err = s.freeUsername(ctx, validate.DefaultUsername(domain)); err != nil {
			return "", 0, err
		}
	default:
		if inUse, err := s.Store.SystemUserInUse(ctx, username); err != nil {
			return "", 0, err
		} else if inUse {
			return "", 0, conflict("username_taken", "username %s is already used by another site", username)
		}
	}
	if err := validate.SiteUsername(username); err != nil {
		return "", 0, badRequest("%v", err)
	}

	tsite := templates.Site{Domain: domain, Aliases: aliases, User: username, PHPVersion: req.PHPVersion}
	site := store.Site{
		Domain: domain, Aliases: aliases, SystemUser: username, RootPath: tsite.Dir() + "/public",
		PHPVersion: req.PHPVersion, Managed: true, Source: "created", WordPress: req.WordPress != nil,
		Status: "creating", NginxConf: templates.VhostPath(domain), FPMPool: templates.PoolPath(req.PHPVersion, domain),
		ParentSiteID: parent.ID,
		// A subdomain site of a site login: PHP shell functions follow the main site.
		BlockPHPShell: parent.BlockPHPShell,
	}
	if req.CreateDB {
		base := username
		if parent.ID != 0 { // the main site's database is already named after the user
			base = username + "_" + strings.TrimSuffix(domain, "."+parent.Domain)
		}
		if site.DBName, err = s.freeDBName(ctx, dbIdent(base)); err != nil {
			return "", 0, err
		}
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
		return s.runCreate(ctx, j, actor, site, aliases, req.WordPress, req.SSL)
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

func (s *Service) runCreate(ctx context.Context, j *jobs.Job, actor agent.Actor, site store.Site, aliases []string, wp *WordPressFields, ssl *SSLFields) (err error) {
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
	if !created.Created { // a subdomain site: the user (and its other sites) stay on rollback
		undo.push("remove site directories", func(ctx context.Context) error {
			return s.Agent.Call(ctx, "site.dirs_remove", actor, actions.DirsParams{Username: site.SystemUser, Domain: site.Domain}, nil)
		})
	}

	j.Step("Configure PHP " + site.PHPVersion + " (FPM pool)")
	s.refreshShellFlag(ctx, &site) // a login may have been created for it meanwhile
	cfg.BlockShell = site.BlockPHPShell
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

	if written := site.BlockPHPShell; s.refreshShellFlag(ctx, &site) && site.BlockPHPShell != written {
		j.Step("Update the PHP pool for the site's login")
		c := s.siteConfig(site, false)
		c.Aliases = aliases
		if err := call("php.pool_write", c); err != nil {
			return err
		}
	}
	site.Status = "active"
	if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
		return err
	}
	if site.ParentSiteID != 0 {
		s.syncAccountSFTP(ctx, j, actor, site.ParentSiteID, 0)
	}
	// The site is live from here on: an SSL failure doesn't roll it back.
	scheme := "http"
	if ssl != nil {
		site.Aliases = aliases
		if err := s.enableHTTPS(ctx, j, actor, &site, ssl.Email); err != nil {
			j.FailStep()
			j.Log("Warning: no certificate yet (%v). The site works over HTTP; once its DNS points to this server, issue one on the SSL tab.", err)
		} else {
			scheme = "https"
		}
	}
	url := scheme + "://" + site.Domain
	if scheme == "http" {
		j.Log("Site is live at %s (point its DNS to this server, then add SSL on the SSL tab)", url)
	} else {
		j.Log("Site is live at %s", url)
	}
	if secrets["wp_admin_url"] != "" {
		secrets["wp_admin_url"] = url + "/wp-admin/"
	}
	j.SetResult(map[string]any{"site_id": site.ID, "domain": site.Domain, "url": url})
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
	if site.ParentSiteID == 0 {
		if n, err := s.Store.CountSubdomains(ctx, site.ID); err != nil {
			return "", err
		} else if n > 0 {
			return "", conflict("has_subdomains", "%s has %d subdomain site(s); delete them first", site.Domain, n)
		}
	}
	// Other sites on the same Linux user (subdomain sites): only this site's
	// folder goes, never the user and its home.
	sharedUser, err := s.Store.SitesUsingUser(ctx, site.SystemUser)
	if err != nil {
		return "", err
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
		if site.ParentSiteID != 0 { // take the folder out of the account's SFTP login
			s.syncAccountSFTP(ctx, j, actor, site.ParentSiteID, site.ID)
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
		switch {
		case req.RemoveFiles && sharedUser > 1:
			dir := templates.Site{Domain: site.Domain, User: site.SystemUser}.Dir()
			j.Step("Remove " + dir)
			if err := s.longCall(ctx, time.Minute, "site.dirs_remove", actor, actions.DirsParams{Username: site.SystemUser, Domain: site.Domain}, nil); err != nil {
				return fail(err)
			}
		case req.RemoveFiles:
			j.Step("Remove Linux user " + site.SystemUser + " and all site files")
			if err := s.longCall(ctx, time.Minute, "site.user_remove", actor, actions.UserParams{Username: site.SystemUser}, nil); err != nil {
				return fail(err)
			}
		default:
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

// refreshShellFlag re-reads the site's PHP shell flag (a login can be
// created while the site is being created). It reports whether it could.
func (s *Service) refreshShellFlag(ctx context.Context, site *store.Site) bool {
	cur, err := s.Store.SiteByID(ctx, site.ID)
	if err != nil {
		return false
	}
	site.BlockPHPShell = cur.BlockPHPShell
	return true
}

// SetPHPShell blocks or allows PHP's shell functions on a site and rewrites
// its pool. A site still being created only gets the flag: its creation
// job writes the pool with it.
func (s *Service) SetPHPShell(ctx context.Context, actor agent.Actor, site store.Site, block bool) error {
	if site.BlockPHPShell == block {
		return nil
	}
	if !site.LayteConfigs() || site.PHPVersion == "" {
		return conflict("site_unmanaged", "%s's PHP pool isn't managed by Layte Panel", site.Domain)
	}
	if err := s.Store.SetBlockPHPShell(ctx, site.ID, block); err != nil {
		return err
	}
	if site.Status == "creating" {
		return nil
	}
	if site.Status != "active" {
		s.Store.SetBlockPHPShell(context.WithoutCancel(ctx), site.ID, site.BlockPHPShell)
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	}
	site.BlockPHPShell = block
	if err := s.Agent.Call(ctx, "php.pool_write", actor, s.siteConfig(site, site.SSL), nil); err != nil {
		s.Store.SetBlockPHPShell(context.WithoutCancel(ctx), site.ID, !block)
		return err
	}
	return nil
}

// BlockAccountShell blocks PHP's shell functions on every site of a new
// site login (main site and subdomains). Sites an admin allowed stay so
// only if it happens after this.
func (s *Service) BlockAccountShell(ctx context.Context, actor agent.Actor, mainID int64) error {
	sites, err := s.Store.SitesForAccount(ctx, mainID)
	if err != nil {
		return err
	}
	for _, x := range sites {
		if !x.LayteConfigs() || x.PHPVersion == "" {
			continue // its pool isn't Layte's (an adopted site still on its own config)
		}
		if err := s.SetPHPShell(ctx, actor, x, true); err != nil {
			return fmt.Errorf("block PHP shell functions on %s: %w", x.Domain, err)
		}
	}
	return nil
}

// subdomainParent checks a new subdomain site against its main site and
// returns the main site.
func (s *Service) subdomainParent(ctx context.Context, parentID int64, domain string, aliases []string) (store.Site, error) {
	parent, err := s.Store.SiteByID(ctx, parentID)
	if errors.Is(err, store.ErrNotFound) {
		return parent, badRequest("no such main site")
	}
	if err != nil {
		return parent, err
	}
	switch {
	case parent.ParentSiteID != 0:
		return parent, badRequest("%s is itself a subdomain site", parent.Domain)
	case !parent.Managed || parent.SystemUser == "":
		return parent, conflict("site_unmanaged", "subdomains can only be added to sites Layte Panel created")
	case parent.Status != "active":
		return parent, conflict("site_busy", "%s is %s", parent.Domain, parent.Status)
	}
	label, ok := strings.CutSuffix(domain, "."+parent.Domain)
	if !ok || label == "" || strings.Contains(label, ".") {
		return parent, badRequest("the domain must be a subdomain of %s, like blog.%s", parent.Domain, parent.Domain)
	}
	if len(aliases) > 0 {
		return parent, badRequest("subdomain sites can't have aliases")
	}
	return parent, nil
}

// freeDBName returns base, or base2, base3, … if a database or site already uses it.
func (s *Service) freeDBName(ctx context.Context, base string) (string, error) {
	sites, err := s.Store.ListSites(ctx)
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, x := range sites {
		taken[x.DBName] = true
	}
	for i := 1; i < 100; i++ {
		n := base
		if i > 1 {
			suffix := fmt.Sprint(i)
			if len(n)+len(suffix) > 32 {
				n = n[:32-len(suffix)]
			}
			n += suffix
		}
		if taken[n] {
			continue
		}
		if _, err := s.Store.DatabaseByName(ctx, n); errors.Is(err, store.ErrNotFound) {
			return n, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", conflict("db_taken", "no free database name for %s", base)
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
