package sites

import (
	"context"
	"slices"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

// SFTPRequest replaces a site's SFTP settings. Keys are public keys as
// pasted (one per entry).
type SFTPRequest struct {
	Enabled  bool     `json:"enabled"`
	Password bool     `json:"password"`
	Keys     []string `json:"keys"`
}

// SFTPState is what the Files tab shows.
type SFTPState struct {
	store.SFTP
	Username string   `json:"username"`
	Folder   string   `json:"folder"`
	Warnings []string `json:"warnings,omitempty"`
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

// SFTP returns the site's SFTP state.
func (s *Service) SFTP(site store.Site) SFTPState {
	return SFTPState{SFTP: site.SFTP, Username: site.SystemUser, Folder: "/" + site.Domain}
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

	p := sftpParams(site)
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
	st := s.SFTP(site)
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
