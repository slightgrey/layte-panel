package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// SiteConfigParams describe a managed site's config files.
type SiteConfigParams struct {
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases,omitempty"`
	Username   string   `json:"username"`
	PHPVersion string   `json:"php_version"`
	SSL        bool     `json:"ssl,omitempty"`
}

func (p SiteConfigParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	aliases, err := validate.Domains(p.Domain, p.Aliases)
	if err != nil {
		return err
	}
	for i := range aliases {
		if aliases[i] != p.Aliases[i] {
			return fmt.Errorf("alias %s must be in normalised form", p.Aliases[i])
		}
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion}) // format; installed is checked in the handler
}

func (p SiteConfigParams) site() templates.Site {
	return templates.Site{Domain: p.Domain, Aliases: p.Aliases, User: p.Username, PHPVersion: p.PHPVersion, SSL: p.SSL}
}

// requireInstalledPHP checks the version against what's really installed.
func (d Deps) requireInstalledPHP(v string) error {
	installed, err := d.phpVersions()
	if err != nil {
		return err
	}
	if err := validate.PHPVersion(v, installed); err != nil {
		return agent.Errorf(agent.CodeInvalidParams, "%v", err)
	}
	return nil
}

func fpmTest(v string) system.Cmd { return system.Cmd{Bin: phpFPMBin(v), Args: []string{"-t"}} }
func fpmReload(v string) system.Cmd {
	return system.Cmd{Bin: systemctl, Args: []string{"reload", phpFPMUnit(v)}}
}

var (
	nginxTest   = system.Cmd{Bin: nginxBin, Args: []string{"-t"}}
	nginxReload = system.Cmd{Bin: systemctl, Args: []string{"reload", "nginx"}}
)

// phpPoolWrite renders and safely installs the site's PHP-FPM pool.
func (d Deps) phpPoolWrite(ctx context.Context, _ agent.Actor, p SiteConfigParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	if _, err := d.Users.Lookup(p.Username); err != nil {
		return nil, err
	}
	content, err := templates.Pool(p.site())
	if err != nil {
		return nil, err
	}
	path := templates.PoolPath(p.PHPVersion, p.Domain)
	err = system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: path, Content: content, Test: fpmTest(p.PHPVersion), Reload: fpmReload(p.PHPVersion),
	})
	if err != nil {
		return nil, configError(err)
	}
	return map[string]string{"path": path}, nil
}

type PoolRemoveParams struct {
	Domain     string `json:"domain"`
	PHPVersion string `json:"php_version"`
}

func (p PoolRemoveParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	return validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion})
}

// phpPoolRemove removes the site's pool for one PHP version (idempotent).
func (d Deps) phpPoolRemove(ctx context.Context, _ agent.Actor, p PoolRemoveParams) (any, error) {
	path := templates.PoolPath(p.PHPVersion, p.Domain)
	if ok, _ := d.FS.Exists(path); !ok {
		return map[string]bool{"removed": false}, nil
	}
	err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{
		Path: path, Test: fpmTest(p.PHPVersion), Reload: fpmReload(p.PHPVersion),
	})
	if err != nil {
		return nil, configError(err)
	}
	return map[string]bool{"removed": true}, nil
}

// nginxVhostWrite renders and safely installs + enables the site's vhost.
func (d Deps) nginxVhostWrite(ctx context.Context, _ agent.Actor, p SiteConfigParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	if p.SSL {
		for _, f := range []string{"fullchain.pem", "privkey.pem"} {
			if ok, _ := d.FS.Exists(templates.CertDir(p.Domain) + "/" + f); !ok {
				return nil, agent.Errorf(agent.CodeInvalidParams, "no certificate for %s yet (%s missing)", p.Domain, f)
			}
		}
	}
	content, err := templates.Vhost(p.site())
	if err != nil {
		return nil, err
	}
	path := templates.VhostPath(p.Domain)
	err = system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: path, Content: content, Link: templates.VhostLink(p.Domain), Test: nginxTest, Reload: nginxReload,
	})
	if err != nil {
		return nil, configError(err)
	}
	return map[string]string{"path": path}, nil
}

type DomainParams struct {
	Domain string `json:"domain"`
}

func (p DomainParams) Validate() error { return validDomain(p.Domain) }

// nginxVhostRemove disables and removes the site's vhost (idempotent).
func (d Deps) nginxVhostRemove(ctx context.Context, _ agent.Actor, p DomainParams) (any, error) {
	err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{
		Path: templates.VhostPath(p.Domain), Link: templates.VhostLink(p.Domain), Test: nginxTest, Reload: nginxReload,
	})
	if err != nil {
		return nil, configError(err)
	}
	return map[string]bool{"removed": true}, nil
}

// configError turns config-test failures into a typed agent error with the
// tool's output, so the UI can show what nginx/php-fpm complained about.
func configError(err error) error {
	var te *system.TestError
	if errors.As(err, &te) {
		return agent.Errorf("config_test_failed", "%s (change rolled back)", te.Error())
	}
	return err
}
