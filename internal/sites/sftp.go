package sites

import (
	"context"
	"slices"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
)

// SFTPRequest replaces a site's SFTP settings. Keys are public keys as
// pasted (one per entry).
type SFTPRequest struct {
	Enabled  bool     `json:"enabled"`
	Password bool     `json:"password"`
	Keys     []string `json:"keys"`
}

// SFTPState is what the Files tab shows. A subdomain site shows its main
// site's login (they share the Linux user): SiteID is the site whose
// settings these are.
type SFTPState struct {
	store.SFTP
	SiteID   int64    `json:"site_id"`
	Domain   string   `json:"domain"`
	Username string   `json:"username"`
	Folder   string   `json:"folder"`
	Folders  []string `json:"folders"`
	Warnings []string `json:"warnings,omitempty"`
}

// SFTPSite is the site that holds the SFTP login for site: itself, or the
// main site of a subdomain site.
func (s *Service) SFTPSite(ctx context.Context, site store.Site) (store.Site, error) {
	if site.ParentSiteID == 0 {
		return site, nil
	}
	return s.Store.SiteByID(ctx, site.ParentSiteID)
}

// accountSFTPParams are a main site's SFTP params with its subdomain sites'
// folders, leaving out the site exclude (one being deleted).
func (s *Service) accountSFTPParams(ctx context.Context, main store.Site, exclude int64) (actions.SFTPParams, error) {
	p := sftpParams(main)
	if main.Adopted {
		return p, nil
	}
	sites, err := s.Store.SitesForAccount(ctx, main.ID)
	if err != nil {
		return p, err
	}
	for _, x := range sites {
		if x.ParentSiteID == main.ID && x.ID != exclude && x.Managed && x.SystemUser == main.SystemUser && x.Status != "creating" {
			p.Extra = append(p.Extra, x.Domain)
		}
	}
	return p, nil
}

// syncAccountSFTP updates a main site's SFTP login after a subdomain site
// was added or (exclude) is being deleted. A failure is only logged: the
// site itself is fine, and saving the SFTP settings again retries it.
func (s *Service) syncAccountSFTP(ctx context.Context, j *jobs.Job, actor agent.Actor, mainID, exclude int64) {
	main, err := s.Store.SiteByID(ctx, mainID)
	if err != nil || !main.SFTP.Enabled {
		return
	}
	p, err := s.accountSFTPParams(ctx, main, exclude)
	if err == nil {
		j.Step("Update the SFTP login")
		for _, k := range main.SFTP.Keys {
			p.Keys = append(p.Keys, k.Key)
		}
		p.Password = main.SFTP.Password
		err = s.longCall(ctx, time.Minute, "sftp.set", actor, p, nil)
	}
	if err != nil {
		j.Log("Warning: couldn't update the SFTP login of %s (%v); save its SFTP settings again to retry.", main.Domain, err)
	}
}

func sftpParams(site store.Site) actions.SFTPParams {
	p := actions.SFTPParams{Domain: site.Domain, Username: site.SystemUser}
	if site.Adopted {
		p.WebRoot = site.RootPath
	}
	return p
}

func requireSFTP(site store.Site) error {
	switch {
	case !site.Managed && !site.Adopted:
		return conflict("site_unmanaged", "%s is read-only here; adopt it to give it an SFTP login", site.Domain)
	case site.SystemUser == "":
		return conflict("site_unmanaged", "%s has no site user", site.Domain)
	case site.Status != "active":
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	}
	return nil
}

// SFTP returns the SFTP state of a main site (see SFTPSite).
func (s *Service) SFTP(ctx context.Context, site store.Site) SFTPState {
	st := SFTPState{SFTP: site.SFTP, SiteID: site.ID, Domain: site.Domain, Username: site.SystemUser, Folder: "/" + site.Domain}
	st.Folders = []string{st.Folder}
	if p, err := s.accountSFTPParams(ctx, site, 0); err == nil {
		for _, d := range p.Extra {
			st.Folders = append(st.Folders, "/"+d)
		}
	}
	return st
}

// SetSFTP turns the site's SFTP login on or off, or changes its keys and
// password option. Keys are kept while it's off, so it can be turned back on.
func (s *Service) SetSFTP(ctx context.Context, actor agent.Actor, site store.Site, req SFTPRequest) (SFTPState, error) {
	if err := requireSFTP(site); err != nil {
		return SFTPState{}, err
	}
	next := store.SFTP{Enabled: req.Enabled, Password: req.Password && req.Enabled, Keys: []store.SFTPKey{}}
	if len(req.Keys) > actions.MaxSFTPKeys {
		return SFTPState{}, badRequest("at most %d keys per site", actions.MaxSFTPKeys)
	}
	for _, k := range req.Keys {
		key, fp, err := actions.NormalizeSSHKey(k)
		if err != nil {
			return SFTPState{}, badRequest("%v", err)
		}
		if !slices.ContainsFunc(next.Keys, func(x store.SFTPKey) bool { return x.Fingerprint == fp }) {
			next.Keys = append(next.Keys, store.SFTPKey{Key: key, Fingerprint: fp})
		}
	}
	if next.Enabled && len(next.Keys) == 0 && !next.Password {
		return SFTPState{}, badRequest("add an SSH key or allow password logins")
	}

	p, err := s.accountSFTPParams(ctx, site, 0)
	if err != nil {
		return SFTPState{}, err
	}
	var res actions.SFTPResult
	if next.Enabled {
		for _, k := range next.Keys {
			p.Keys = append(p.Keys, k.Key)
		}
		p.Password = next.Password
		if err := s.longCall(ctx, time.Minute, "sftp.set", actor, p, &res); err != nil {
			return SFTPState{}, err
		}
	} else if site.SFTP.Enabled {
		if err := s.longCall(ctx, time.Minute, "sftp.disable", actor, p, nil); err != nil {
			return SFTPState{}, err
		}
	}
	site.SFTP = next
	if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
		return SFTPState{}, err
	}
	st := s.SFTP(ctx, site)
	st.Warnings = res.Warnings
	return st, nil
}

// NewSFTPPassword sets and returns a new random password for the site's SFTP
// login. It is shown once and never stored.
func (s *Service) NewSFTPPassword(ctx context.Context, actor agent.Actor, site store.Site) (string, error) {
	if err := requireSFTP(site); err != nil {
		return "", err
	}
	if !site.SFTP.Enabled || !site.SFTP.Password {
		return "", badRequest("turn on SFTP with password logins first")
	}
	var out actions.SFTPPassword
	if err := s.longCall(ctx, 30*time.Second, "sftp.password", actor, sftpParams(site), &out); err != nil {
		return "", err
	}
	return out.Password, nil
}
