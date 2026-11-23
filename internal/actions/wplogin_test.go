package actions

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

var shopLogin = WPLoginParams{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/shop/public", PHPVersion: "8.2"}

const shopPlugin = "/var/www/shop/public/" + wpLoginPluginRel

func TestWPLoginToken(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	d.FS.MkdirAll("/var/www/shop/public/wp-content", 0o755)

	res, err := d.wpLoginToken(ctx, agent.Actor{}, shopLogin)
	if err != nil {
		t.Fatal(err)
	}
	login := res.(WPLogin)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(login.Token) || login.UserID != 1 {
		t.Errorf("login = %+v", login)
	}
	if login.ActionURL != "http://shop.example.org/wp-login.php" && login.ActionURL != "https://shop.example.org/wp-login.php" {
		t.Errorf("action URL %q", login.ActionURL)
	}
	if b, err := d.FS.ReadFile(shopPlugin); err != nil || string(b) != wpLoginPlugin {
		t.Errorf("plugin not installed: %v", err)
	}

	// Only a hash of the token is stored, for 60 seconds.
	sum := sha256.Sum256([]byte(login.Token))
	key := "layte_login_" + hex.EncodeToString(sum[:])[:40]
	calls := strings.Join(d.Runner.(*system.FakeRunner).CallList(), "\n")
	if !strings.Contains(calls, "transient set "+key+" 1 60") {
		t.Errorf("transient not set for %s:\n%s", key, calls)
	}
	if strings.Contains(calls, login.Token) {
		t.Error("the raw token was passed to a command")
	}

	// A second call makes a different token.
	res2, err := d.wpLoginToken(ctx, agent.Actor{}, shopLogin)
	if err != nil || res2.(WPLogin).Token == login.Token {
		t.Errorf("second token: %v", err)
	}
}

// A file of the same name that Layte didn't write is left alone.
func TestWPLoginKeepsForeignPlugin(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	d.FS.MkdirAll("/var/www/shop/public/wp-content/mu-plugins", 0o755)
	foreign := []byte("<?php // someone else's login plugin\n")
	d.FS.WriteFileAtomic(shopPlugin, foreign, 0o644)

	if _, err := d.wpLoginToken(ctx, agent.Actor{}, shopLogin); err == nil {
		t.Fatal("replaced a plugin Layte didn't write")
	}
	if b, _ := d.FS.ReadFile(shopPlugin); string(b) != string(foreign) {
		t.Error("foreign plugin changed")
	}

	// An older Layte version of the plugin is updated.
	d.FS.WriteFileAtomic(shopPlugin, []byte("<?php\n// Managed by Layte Panel — old version\n"), 0o644)
	if _, err := d.wpLoginToken(ctx, agent.Actor{}, shopLogin); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.FS.ReadFile(shopPlugin); string(b) != wpLoginPlugin {
		t.Error("old Layte plugin not updated")
	}
}

func TestWPLoginNeedsWordPress(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d) // no wp-content
	if _, err := d.wpLoginToken(ctx, agent.Actor{}, shopLogin); err == nil {
		t.Error("accepted a site without wp-content")
	}
	bad := shopLogin
	bad.Domain = "../etc"
	if bad.Validate() == nil {
		t.Error("accepted a bad domain")
	}
}
