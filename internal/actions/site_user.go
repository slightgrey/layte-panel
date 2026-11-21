package actions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// siteUserMarker is created in the home of every user Layte creates. Only
// users with this file can be deleted by Layte.
const siteUserMarker = ".layte-site-user"

type UserParams struct {
	Username string `json:"username"`
}

func (p UserParams) Validate() error { return validate.SiteUsername(p.Username) }

// siteUserCreate creates the Linux user for a site (no login shell) and lets
// nginx (www-data) read its files through group membership. Calling it again
// for a user Layte already created is a no-op.
func (d Deps) siteUserCreate(ctx context.Context, _ agent.Actor, p UserParams) (any, error) {
	home := "/home/" + p.Username
	if _, err := d.Users.Lookup(p.Username); err == nil {
		if ok, _ := d.FS.Exists(home + "/" + siteUserMarker); ok {
			return map[string]bool{"created": false}, nil
		}
		return nil, agent.Errorf(agent.CodeInvalidParams, "Linux user %q already exists and wasn't created by Layte Panel; choose another username", p.Username)
	} else if !errors.Is(err, system.ErrNoUser) {
		return nil, err
	}

	if res, err := d.Runner.Run(ctx, useraddBin, "--create-home", "--home-dir", home, "--shell", "/usr/sbin/nologin", "--user-group", p.Username); err != nil {
		return nil, fmt.Errorf("useradd %s: %s", p.Username, res.Output())
	}
	if err := d.FS.Chmod(home, 0o750); err != nil {
		return nil, fmt.Errorf("chmod %s: %w", home, err)
	}
	if err := d.FS.WriteFileAtomic(home+"/"+siteUserMarker, []byte("created by Layte Panel\n"), 0o644); err != nil {
		return nil, err
	}
	if res, err := d.Runner.Run(ctx, usermodBin, "--append", "--groups", p.Username, "www-data"); err != nil {
		return nil, fmt.Errorf("add www-data to group %s: %s", p.Username, res.Output())
	}
	return map[string]bool{"created": true}, nil
}

// siteUserRemove deletes a user Layte created, with its home directory and
// all site files. Users without the Layte marker are refused.
func (d Deps) siteUserRemove(ctx context.Context, _ agent.Actor, p UserParams) (any, error) {
	if _, err := d.Users.Lookup(p.Username); errors.Is(err, system.ErrNoUser) {
		return map[string]bool{"removed": false}, nil
	} else if err != nil {
		return nil, err
	}
	if ok, _ := d.FS.Exists("/home/" + p.Username + "/" + siteUserMarker); !ok {
		return nil, agent.Errorf(agent.CodeInvalidParams, "user %q wasn't created by Layte Panel; refusing to delete it", p.Username)
	}
	// Drop www-data from the user's group first, so userdel can remove the group.
	d.Runner.Run(ctx, gpasswdBin, "--delete", "www-data", p.Username)
	if res, err := d.Runner.Run(ctx, userdelBin, "--force", "--remove", p.Username); err != nil {
		return nil, fmt.Errorf("userdel %s: %s", p.Username, res.Output())
	}
	return map[string]bool{"removed": true}, nil
}

type DirsParams struct {
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

func (p DirsParams) Validate() error {
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	return validDomain(p.Domain)
}

// siteDirsCreate creates /home/<user>/<domain>/{public,logs,tmp} owned by the
// site user, plus a placeholder index.html.
func (d Deps) siteDirsCreate(ctx context.Context, _ agent.Actor, p DirsParams) (any, error) {
	cred, err := d.Users.Lookup(p.Username)
	if err != nil {
		return nil, err
	}
	site := templates.Site{Domain: p.Domain, User: p.Username}
	dirs := []string{site.Dir(), site.Dir() + "/public", site.Dir() + "/logs", site.Dir() + "/tmp"}
	for _, dir := range dirs {
		if err := d.FS.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		// Never chown through a symlink (it could point anywhere).
		fi, err := d.FS.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a plain directory", dir)
		}
		if err := d.FS.Chown(dir, int(cred.UID), int(cred.GID)); err != nil {
			return nil, fmt.Errorf("chown %s: %w", dir, err)
		}
		if err := d.FS.Chmod(dir, 0o750); err != nil {
			return nil, err
		}
	}
	index := site.Dir() + "/public/index.html"
	if ok, _ := d.FS.Exists(index); !ok {
		page := fmt.Sprintf("<!doctype html>\n<title>%s</title>\n<h1>%s is ready</h1>\n<p>Upload your site to %s/public.</p>\n", p.Domain, p.Domain, site.Dir())
		if err := d.FS.WriteFileAtomic(index, []byte(page), 0o640); err != nil {
			return nil, err
		}
		d.FS.Chown(index, int(cred.UID), int(cred.GID))
	}
	return map[string]string{"dir": site.Dir()}, nil
}

// validDomain requires an already-normalised domain (the API normalises; the
// agent only accepts the canonical form).
func validDomain(domain string) error {
	norm, err := validate.Domain(domain)
	if err != nil {
		return err
	}
	if norm != domain {
		return fmt.Errorf("domain must be in normalised form (%s)", norm)
	}
	return nil
}
