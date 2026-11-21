package sites

import (
	"context"
	"errors"
	"slices"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// DatabaseView is one row on the Databases page.
type DatabaseView struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	System     bool   `json:"system"`
	Managed    bool   `json:"managed"` // created by Layte: can be dropped / password reset
	User       string `json:"user,omitempty"`
	SiteID     int64  `json:"site_id,omitempty"`
	SiteDomain string `json:"site_domain,omitempty"`
	WordPress  bool   `json:"wordpress,omitempty"`
}

// ListDatabases merges the server's databases with Layte's records.
func (s *Service) ListDatabases(ctx context.Context, actor agent.Actor) (engine string, out []DatabaseView, err error) {
	var res struct {
		Engine    string           `json:"engine"`
		Databases []actions.DBInfo `json:"databases"`
	}
	if err := s.Agent.Call(ctx, "db.list", actor, nil, &res); err != nil {
		return "", nil, err
	}
	owned, err := s.Store.ListDatabases(ctx)
	if err != nil {
		return "", nil, err
	}
	sites, err := s.Store.ListSites(ctx)
	if err != nil {
		return "", nil, err
	}
	byID := map[int64]store.Site{}
	for _, x := range sites {
		byID[x.ID] = x
	}
	byName := map[string]store.Database{}
	for _, d := range owned {
		byName[d.Name] = d
	}
	out = []DatabaseView{}
	for _, d := range res.Databases {
		v := DatabaseView{Name: d.Name, Size: d.Size, System: d.System}
		if o, ok := byName[d.Name]; ok {
			v.Managed, v.User, v.SiteID = true, o.User, o.SiteID
			if site, ok := byID[o.SiteID]; ok {
				v.SiteDomain, v.WordPress = site.Domain, site.WordPress
			}
		}
		out = append(out, v)
	}
	return res.Engine, out, nil
}

// CreateDatabase creates a database plus a user of the same name and returns
// the generated password (shown once, never stored).
func (s *Service) CreateDatabase(ctx context.Context, actor agent.Actor, name string, siteID int64) (string, error) {
	if err := validate.DBIdent(name); err != nil {
		return "", badRequest("%v", err)
	}
	if slices.Contains([]string{"mysql", "sys", "information_schema", "performance_schema"}, name) {
		return "", badRequest("%s is a system database name", name)
	}
	if siteID > 0 {
		site, err := s.Store.SiteByID(ctx, siteID)
		if err != nil {
			return "", badRequest("no such site")
		}
		if !site.Managed {
			return "", conflict("site_unmanaged", "databases can only be linked to sites created by Layte Panel")
		}
	}
	if _, err := s.Store.DatabaseByName(ctx, name); err == nil {
		return "", conflict("db_exists", "database %s already exists", name)
	}
	pw := randomPassword()
	if err := s.Agent.Call(ctx, "db.create", actor, actions.DBCreateParams{Name: name, User: name, Password: pw}, nil); err != nil {
		if agent.IsCode(err, agent.CodeInvalidParams) {
			return "", conflict("db_exists", "%v", err)
		}
		return "", err
	}
	if err := s.Store.AddDatabase(ctx, store.Database{Name: name, User: name, SiteID: siteID}); err != nil {
		return "", err
	}
	return pw, nil
}

// managedDB loads a Layte database and its site (if any).
func (s *Service) managedDB(ctx context.Context, name string) (store.Database, *store.Site, error) {
	db, err := s.Store.DatabaseByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return db, nil, conflict("db_unmanaged", "%s wasn't created by Layte Panel, so it is read-only here", name)
	}
	if err != nil {
		return db, nil, err
	}
	if db.SiteID > 0 {
		if site, err := s.Store.SiteByID(ctx, db.SiteID); err == nil {
			return db, &site, nil
		}
	}
	return db, nil, nil
}

// DropDatabase drops a Layte database that isn't used by a site.
func (s *Service) DropDatabase(ctx context.Context, actor agent.Actor, name, confirm string) error {
	db, site, err := s.managedDB(ctx, name)
	if err != nil {
		return err
	}
	if site != nil {
		return conflict("db_in_use", "%s belongs to site %s; delete the site (with its database) instead", name, site.Domain)
	}
	if confirm != name {
		return badRequest("type the database name (%s) to confirm", name)
	}
	if err := s.Agent.Call(ctx, "db.drop", actor, actions.DBDropParams{Name: db.Name, User: db.User}, nil); err != nil {
		return err
	}
	return s.Store.RemoveDatabase(ctx, name)
}

// ResetDBPassword sets a new random password and returns it (shown once).
// WordPress sites are refused: their wp-config.php would stop working.
func (s *Service) ResetDBPassword(ctx context.Context, actor agent.Actor, name string) (string, error) {
	db, site, err := s.managedDB(ctx, name)
	if err != nil {
		return "", err
	}
	if site != nil && site.WordPress {
		return "", conflict("db_in_use", "%s is used by the WordPress site %s; changing it here would break wp-config.php", name, site.Domain)
	}
	pw := randomPassword()
	if err := s.Agent.Call(ctx, "db.password_reset", actor, actions.DBPasswordParams{User: db.User, Password: pw}, nil); err != nil {
		return "", err
	}
	return pw, nil
}
