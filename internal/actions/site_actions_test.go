package actions

import (
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
	if _, err := d.dbCreate(ctx, agent.Actor{}, DBCreateParams{Name: "shop", User: "shop", Password: pw}); err != nil {
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
