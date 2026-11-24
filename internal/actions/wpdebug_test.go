package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func wpCalls(d Deps) string {
	var out []string
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		if strings.Contains(c, " config ") {
			out = append(out, c)
		}
	}
	return strings.Join(out, "\n")
}

func TestWPDebugAdopted(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)

	got, err := d.wpDebugGet(ctx, agent.Actor{}, shopLogin)
	if err != nil {
		t.Fatal(err)
	}
	if got.(WPDebugState).Enabled {
		t.Error("enabled before turning it on")
	}

	out, err := d.wpDebugSet(ctx, agent.Actor{}, WPDebugSetParams{WPLoginParams: shopLogin, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.(WPDebugState).Enabled {
		t.Errorf("state %+v", out)
	}
	calls := wpCalls(d)
	for _, want := range []string{"config set WP_DEBUG true --type=constant --raw", "config set WP_DEBUG_DISPLAY false --type=constant --raw",
		"config set WP_DEBUG_LOG false --type=constant --raw"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in:\n%s", want, calls)
		}
	}
	if got, _ := d.wpDebugGet(ctx, agent.Actor{}, shopLogin); !got.(WPDebugState).Enabled {
		t.Error("not enabled after turning it on")
	}

	if _, err := d.wpDebugSet(ctx, agent.Actor{}, WPDebugSetParams{WPLoginParams: shopLogin, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.wpDebugGet(ctx, agent.Actor{}, shopLogin); got.(WPDebugState).Enabled {
		t.Error("still enabled after turning it off")
	}
}

func TestWPDebugPoolErrorLog(t *testing.T) {
	pool := "[shop]\nuser = shop\nphp_admin_value[error_log] = /var/log/shop/php-error.log\n"
	if got := poolErrorLog(pool); got != "/var/log/shop/php-error.log" {
		t.Errorf("poolErrorLog = %q", got)
	}
	if got := poolErrorLog("[shop]\n; php_admin_value[error_log] = /x.log\n"); got != "" {
		t.Errorf("commented out: %q", got)
	}
}

// If the site answers differently afterwards, wp-config.php is put back.
func TestWPDebugRollsBack(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	orig, _ := d.FS.ReadFile("/var/www/shop/public/wp-config.php")
	calls := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		calls++
		if calls == 1 {
			return 200, nil
		}
		return 500, nil
	}
	// The fake WP-CLI doesn't edit wp-config.php, so simulate a change.
	inner := d.Runner.(*system.FakeRunner).Handle
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), " config set ") {
			d.FS.WriteFileAtomic("/var/www/shop/public/wp-config.php", []byte("<?php // broken\n"), 0o640)
		}
		return inner(c)
	}}
	_, err := d.wpDebugSet(ctx, agent.Actor{}, WPDebugSetParams{WPLoginParams: shopLogin, Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "put back") {
		t.Fatalf("err = %v", err)
	}
	if now, _ := d.FS.ReadFile("/var/www/shop/public/wp-config.php"); string(now) != string(orig) {
		t.Errorf("wp-config.php not restored:\n%s", now)
	}
}

// Servers that only include conf.d (no sites-enabled) still list site logs.
func TestLogSourcesReadConfD(t *testing.T) {
	d := devDeps(t)
	d.FS.MkdirAll("/home/acme/a.test/logs", 0o755)
	d.FS.WriteFileAtomic("/home/acme/a.test/logs/wp-debug.log", nil, 0o644)
	d.FS.WriteFileAtomic("/etc/nginx/conf.d/a.test.conf", []byte(`server {
    listen 80;
    server_name a.test;
    root /home/acme/a.test/public;
}
`), 0o644)
	out, err := d.logsSources(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range out.([]LogSource) {
		if s.Path == "/home/acme/a.test/logs/wp-debug.log" && s.Domain == "a.test" {
			return
		}
	}
	t.Errorf("wp-debug.log not listed: %+v", out)
}
