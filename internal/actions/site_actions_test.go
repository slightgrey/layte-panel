package actions

import (
	"path"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestSiteUserRefusesForeignUsers(t *testing.T) {
	d := devDeps(t)
	d.FS.MkdirAll("/home/bob", 0o750) // exists, but not created by Layte

	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "bob"}); err == nil {
		t.Error("adopted an existing non-Layte user")
	}
	if _, err := d.siteUserRemove(ctx, agent.Actor{}, UserParams{Username: "bob"}); err == nil {
		t.Error("deleted a non-Layte user")
	}
	if ok, _ := d.FS.Exists("/home/bob"); !ok {
		t.Error("bob's home was removed")
	}

	// A Layte user: create is idempotent, remove works.
	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	out, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"})
	if err != nil || out.(map[string]bool)["created"] {
		t.Errorf("second create: %v %v", out, err)
	}
	if _, err := d.siteUserRemove(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists("/home/acme"); ok {
		t.Error("acme not removed")
	}
}

func TestParamValidation(t *testing.T) {
	tests := []struct {
		name string
		v    agent.Validator
	}{
		{"user root", UserParams{Username: "root"}},
		{"dir domain traversal", DirsParams{Username: "acme", Domain: "../etc"}},
		{"non-normalised domain", DirsParams{Username: "acme", Domain: "Shop.Test"}},
		{"bad php", SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3;id"}},
		{"bad alias", SiteConfigParams{Domain: "a.test", Aliases: []string{"*.a.test"}, Username: "acme", PHPVersion: "8.3"}},
		{"system db", DBCreateParams{Name: "mysql", User: "x", Password: strings.Repeat("a", 32)}},
		{"sql in name", DBCreateParams{Name: "a`; DROP", User: "x", Password: strings.Repeat("a", 32)}},
		{"quote in password", DBCreateParams{Name: "a", User: "x", Password: strings.Repeat("a", 30) + "'"}},
		{"drop root", DBDropParams{Name: "a", User: "root"}},
		{"wp wrong url", WPInstallParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3", URL: "http://evil.test",
			Title: "t", AdminUser: "owner", AdminEmail: "o@a.test", AdminPassword: strings.Repeat("a", 32),
			DBName: "a", DBUser: "a", DBPassword: strings.Repeat("a", 32)}},
	}
	for _, tt := range tests {
		if tt.v.Validate() == nil {
			t.Errorf("%s: accepted", tt.name)
		}
	}
}

func TestDBCreateUsesStdin(t *testing.T) {
	d := devDeps(t)
	pw := "Secret0Secret0Secret0Secret0AB"
	if _, err := d.dbCreate(ctx, agent.Actor{}, DBCreateParams{Name: "shopx", User: "shopx", Password: pw}); err != nil {
		t.Fatal(err)
	}
	fr := d.Runner.(*system.FakeRunner)
	last := fr.Cmds[len(fr.Cmds)-1]
	if strings.Contains(last.String(), pw) || !strings.Contains(string(last.Stdin), "IDENTIFIED BY '"+pw+"'") {
		t.Errorf("cmd = %s, stdin = %q", last.String(), last.Stdin)
	}
}

func TestPoolAndVhostNeedInstalledPHP(t *testing.T) {
	d := devDeps(t)
	d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"})
	p := SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "7.4"}
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, p); !agent.IsCode(err, agent.CodeInvalidParams) {
		t.Errorf("pool for missing PHP: %v", err)
	}
	if _, err := d.nginxVhostWrite(ctx, agent.Actor{}, p); !agent.IsCode(err, agent.CodeInvalidParams) {
		t.Errorf("vhost for missing PHP: %v", err)
	}
}

func TestVhostFilesFollowsNginxIncludes(t *testing.T) {
	const avail, link, confd = "/etc/nginx/sites-available/layte-a.test.conf", "/etc/nginx/sites-enabled/layte-a.test.conf", "/etc/nginx/conf.d/layte-a.test.conf"
	tests := []struct {
		name, nginxConf, existing string
		wantPath, wantLink        string
	}{
		{"no nginx.conf: default layout", "", "", avail, link},
		{"includes sites-enabled", "http {\n    include /etc/nginx/conf.d/*.conf;\n    include /etc/nginx/sites-enabled/*;\n}\n", "", avail, link},
		{"relative include", "http {\n\tinclude sites-enabled/*;\n}\n", "", avail, link},
		{"conf.d only", "http {\n    include /etc/nginx/conf.d/*.conf;\n}\n", "", confd, ""},
		{"sites-enabled commented out", "http {\n    include /etc/nginx/conf.d/*.conf;\n    # include /etc/nginx/sites-enabled/*;\n}\n", "", confd, ""},
		{"existing conf.d vhost stays", "http { include /etc/nginx/sites-enabled/*; }\n", confd, confd, ""},
		{"existing sites-available vhost stays", "http {\n    include /etc/nginx/conf.d/*.conf;\n}\n", avail, avail, link},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := devDeps(t)
			d.FS.Remove("/etc/nginx/nginx.conf")
			if tt.nginxConf != "" {
				d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte(tt.nginxConf), 0o644)
			}
			if tt.existing != "" {
				d.FS.MkdirAll(path.Dir(tt.existing), 0o755)
				d.FS.WriteFileAtomic(tt.existing, []byte(system.Marker+"\n"), 0o644)
			}
			if p, l := d.vhostFiles("a.test"); p != tt.wantPath || l != tt.wantLink {
				t.Errorf("got (%q, %q), want (%q, %q)", p, l, tt.wantPath, tt.wantLink)
			}
		})
	}
}

// On a server whose nginx only loads conf.d, the vhost must land there (and
// get no sites-enabled link), and removal must find it again.
func TestVhostWriteRemoveConfDOnly(t *testing.T) {
	d := devDeps(t)
	d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte("http {\n    include /etc/nginx/conf.d/*.conf;\n}\n"), 0o644)
	d.FS.MkdirAll("/etc/nginx/conf.d", 0o755)
	d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"})
	p := SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3"}

	out, err := d.nginxVhostWrite(ctx, agent.Actor{}, p)
	if err != nil {
		t.Fatal(err)
	}
	const confd = "/etc/nginx/conf.d/layte-a.test.conf"
	if got := out.(map[string]string)["path"]; got != confd {
		t.Errorf("path = %q", got)
	}
	for _, f := range []string{"/etc/nginx/sites-available/layte-a.test.conf", "/etc/nginx/sites-enabled/layte-a.test.conf"} {
		if ok, _ := d.FS.Exists(f); ok {
			t.Errorf("%s written on a conf.d-only server", f)
		}
	}
	if _, err := d.nginxVhostRemove(ctx, agent.Actor{}, DomainParams{Domain: "a.test"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(confd); ok {
		t.Error("vhost not removed")
	}
}

func TestWebUserFromNginxConf(t *testing.T) {
	tests := []struct {
		name, nginxConf, existingUser, want string
	}{
		{"no nginx.conf", "", "", "www-data"},
		{"ubuntu default", "user www-data;\nworker_processes auto;\n", "", "www-data"},
		{"nginx user", "user nginx;\nworker_processes auto;\n", "nginx", "nginx"},
		{"user with group", "user  nginx nginx;\n", "nginx", "nginx"},
		{"commented out", "# user nginx;\n", "nginx", "www-data"},
		{"user doesn't exist", "user ghost;\n", "", "www-data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := devDeps(t)
			d.FS.Remove("/etc/nginx/nginx.conf")
			if tt.nginxConf != "" {
				d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte(tt.nginxConf), 0o644)
			}
			if tt.existingUser != "" {
				d.FS.MkdirAll("/home/"+tt.existingUser, 0o755) // DirUsers: a home dir = the user exists
			}
			if got := d.webUser(); got != tt.want {
				t.Errorf("webUser() = %q, want %q", got, tt.want)
			}
		})
	}
}

// When nginx runs as "nginx", that user must own the FPM socket and join
// the site's group; otherwise nginx gets "permission denied" (a 404 page).
func TestSiteUsesNginxWorkerUser(t *testing.T) {
	d := devDeps(t)
	d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte("user nginx;\nhttp { include /etc/nginx/sites-enabled/*; }\n"), 0o644)
	d.FS.MkdirAll("/home/nginx", 0o755)

	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	fr := d.Runner.(*system.FakeRunner)
	if last := fr.Cmds[len(fr.Cmds)-1].String(); !strings.Contains(last, "--groups acme nginx") {
		t.Errorf("last cmd = %s", last)
	}
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	pool, _ := d.FS.ReadFile("/etc/php/8.3/fpm/pool.d/layte-a.test.conf")
	if !strings.Contains(string(pool), "listen.owner = nginx\nlisten.group = nginx\n") {
		t.Errorf("pool:\n%s", pool)
	}
}
