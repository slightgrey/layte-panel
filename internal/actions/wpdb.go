package actions

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Changing a WordPress site's database password: the MySQL user and
// wp-config.php must change together, or the site goes down. The old
// password is read from wp-config.php (as the site user) and only ever used
// inside the agent, to undo the change if the site stops answering.

// DB_PASSWORD with a value we can rewrite safely: single quotes without ' or
// \, or double quotes without ", \ or $ (no escapes, no PHP interpolation).
var (
	wpPassSingleRe = regexp.MustCompile(`define\(\s*['"]DB_PASSWORD['"]\s*,\s*'([^'\\]*)'\s*\)`)
	wpPassDoubleRe = regexp.MustCompile(`define\(\s*['"]DB_PASSWORD['"]\s*,\s*"([^"\\$]*)"\s*\)`)
	wpAnyPassRe    = regexp.MustCompile(`define\(\s*['"]DB_PASSWORD['"]`)
	wpNameRe       = regexp.MustCompile(`define\(\s*['"]DB_NAME['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	wpUserRe       = regexp.MustCompile(`define\(\s*['"]DB_USER['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
)

type WPDBPasswordParams struct {
	Database string `json:"database"`
	User     string `json:"user"` // the MySQL user wp-config.php uses
	Domain   string `json:"domain"`
	Username string `json:"username"`           // site's Linux user
	WebRoot  string `json:"web_root,omitempty"` // adopted sites
	Password string `json:"password"`           // the new one; redacted in the audit log
}

func (p WPDBPasswordParams) Validate() error {
	for _, id := range []string{p.Database, p.User} {
		if err := validate.DBIdent(id); err != nil {
			return err
		}
	}
	if slices.Contains(systemDatabases, p.Database) || p.User == "root" {
		return fmt.Errorf("%s is reserved", p.Database)
	}
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.WebRoot != "" {
		if err := (FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}).validate(readOps); err != nil {
			return err
		}
	}
	return validate.Password(p.Password)
}

// wpPassword finds the one DB_PASSWORD define and returns its value and a
// function that rewrites it. ok is false if there isn't exactly one we can
// safely rewrite.
func wpPassword(src string) (old string, replace func(newPW string) string, ok bool) {
	if len(wpAnyPassRe.FindAllStringIndex(src, -1)) != 1 {
		return "", nil, false
	}
	for _, re := range []*regexp.Regexp{wpPassSingleRe, wpPassDoubleRe} {
		if m := re.FindStringSubmatchIndex(src); m != nil {
			old = src[m[2]:m[3]]
			return old, func(newPW string) string { return src[:m[2]] + newPW + src[m[3]:] }, true
		}
	}
	return "", nil, false
}

// sqlString quotes a value for a single-quoted SQL literal.
func sqlString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s)
}

// onlyGrantsOn checks that user@localhost can reach db and nothing else, so
// changing its password can't break another site.
func (d Deps) onlyGrantsOn(ctx context.Context, user, db string) error {
	out, err := d.mysqlQuery(ctx, fmt.Sprintf("SHOW GRANTS FOR '%s'@'localhost';\n", user))
	if err != nil {
		return agent.Errorf(agent.CodeNotFound, "MySQL user %s@localhost not found", user)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		on := ""
		if i := strings.Index(line, " ON "); i >= 0 {
			on = line[i+4:]
			if j := strings.Index(on, " TO "); j >= 0 {
				on = on[:j]
			}
		}
		switch {
		case strings.HasPrefix(line, "GRANT USAGE ON *.* TO "):
		case on == "`"+db+"`.*" || on == db+".*":
		default:
			return agent.Errorf(agent.CodeInvalidParams, "MySQL user %s has other rights too (%s); changing its password could break another site", user, strings.TrimSpace(strings.SplitN(line, " TO ", 2)[0]))
		}
	}
	return nil
}

// wpDBPassword sets a new password for a WordPress site's database user and
// writes it into wp-config.php, then checks the site answers as before.
// Any failure puts the old password and the old wp-config.php back.
func (d Deps) wpDBPassword(ctx context.Context, actor agent.Actor, p WPDBPasswordParams) (any, error) {
	root, as, _, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot})
	if err != nil {
		return nil, err
	}
	rel := "public/wp-config.php" // Layte sites: the site folder holds public/
	if p.WebRoot != "" {
		rel = "wp-config.php"
	}
	file := func(req fileops.Request) (fileops.Response, error) {
		req.Root, req.Path = root, rel
		res, err := d.FileOps.Run(ctx, as, req)
		if err == nil && res.Error != "" {
			err = agent.Errorf("fs_error", "%s: %s", rel, res.Error)
		}
		return res, err
	}
	read, err := file(fileops.Request{Op: "read", Length: 256 << 10})
	if err != nil {
		return nil, err
	}
	if read.Size > 256<<10 {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s is unexpectedly large", rel)
	}
	src := string(read.Data)
	if m := wpNameRe.FindStringSubmatch(src); m == nil || m[1] != p.Database {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s doesn't use the database %s", rel, p.Database)
	}
	if m := wpUserRe.FindStringSubmatch(src); m == nil || m[1] != p.User {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s doesn't use the MySQL user %s", rel, p.User)
	}
	oldPW, replace, ok := wpPassword(src)
	if !ok {
		return nil, agent.Errorf(agent.CodeInvalidParams, "can't safely update DB_PASSWORD in %s (it must be a single plain quoted value)", rel)
	}
	if err := d.onlyGrantsOn(ctx, p.User, p.Database); err != nil {
		return nil, err
	}
	site, err := d.discoveredSite(ctx, p.Domain)
	if err != nil {
		return nil, err
	}
	before, err := d.siteStatus(ctx, site)
	if err != nil {
		return nil, agent.Errorf("site_check_failed", "%s doesn't answer before the change (%v); fix that first", p.Domain, err)
	}

	setPW := func(ctx context.Context, pw string) error {
		return d.mysql(ctx, fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';\n", p.User, sqlString(pw)))
	}
	bg := context.WithoutCancel(ctx)
	undoPW := func() { setPW(bg, oldPW) }
	if err := setPW(ctx, p.Password); err != nil {
		return nil, err
	}
	if _, err := file(fileops.Request{Op: "write", Chunk: []byte(replace(p.Password)), Final: true}); err != nil {
		undoPW()
		return nil, err
	}
	undoFile := func() {
		d.FileOps.Run(bg, as, fileops.Request{Op: "write", Root: root, Path: rel, Chunk: read.Data, Final: true})
	}
	after, err := d.siteStatus(ctx, site)
	if err != nil || after != before {
		undoFile()
		undoPW()
		return nil, agent.Errorf("site_check_failed", "%s answered %d before and %d after the change; the old password and wp-config.php were put back", p.Domain, before, after)
	}
	return map[string]any{"changed": true, "status": after}, nil
}

// discoveredSite is a domain from the nginx config (for its HTTPS flag).
func (d Deps) discoveredSite(ctx context.Context, domain string) (DiscoveredSite, error) {
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		return DiscoveredSite{}, err
	}
	for _, s := range out.([]DiscoveredSite) {
		if s.Domain == domain {
			return s, nil
		}
	}
	return DiscoveredSite{}, agent.Errorf(agent.CodeNotFound, "%s isn't in the nginx config", domain)
}
