package actions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestFPMGraceful(t *testing.T) {
	d := devDeps(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// The admin sets the timeout in 8.2's php-fpm.conf: left alone.
	d.FS.WriteFileAtomic("/etc/php/8.2/fpm/php-fpm.conf", []byte("[global]\nprocess_control_timeout = 20s\n"), 0o644)
	d.FS.WriteFileAtomic("/etc/php/8.3/fpm/php-fpm.conf", []byte("[global]\n;process_control_timeout = 0\ninclude=/etc/php/8.3/fpm/pool.d/*.conf\n"), 0o644)
	d.EnsureFPMGraceful(ctx, log)

	b, err := d.FS.ReadFile(fpmGracefulPath("8.3"))
	if err != nil || !system.IsManaged(b) || !strings.Contains(string(b), "[global]\nprocess_control_timeout = 10s\n") {
		t.Errorf("8.3 file: %q %v", b, err)
	}
	if ok, _ := d.FS.Exists(fpmGracefulPath("8.2")); ok {
		t.Error("wrote a file although php-fpm.conf sets the timeout")
	}
	// Running again changes nothing (no php-fpm -t either).
	fr := d.Runner.(*system.FakeRunner)
	n := len(fr.CallList())
	d.EnsureFPMGraceful(ctx, log)
	if len(fr.CallList()) != n {
		t.Errorf("second run ran commands: %v", fr.CallList()[n:])
	}
	// It is no pool: discovery and adoption ignore it.
	if pools := d.findPools(""); len(pools) != 0 {
		t.Errorf("graceful file seen as a pool: %+v", pools)
	}
}

func TestPoolWriteAddsFPMGraceful(t *testing.T) {
	d := devDeps(t)
	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(fpmGracefulPath("8.3")); !ok {
		t.Error("pool write didn't add the graceful reload file")
	}
}

func TestPoolWriteWaitsForSocket(t *testing.T) {
	d := devDeps(t)
	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	var waited string
	d.WaitSocket = func(_ context.Context, path string) error { waited = path; return nil }
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	if waited != "/run/php/layte-a.test-php8.3.sock" {
		t.Errorf("waited for %q", waited)
	}
	d.WaitSocket = func(context.Context, string) error { return errors.New("connection refused") }
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, SiteConfigParams{Domain: "a.test", Username: "acme", PHPVersion: "8.2"}); err == nil ||
		!strings.Contains(err.Error(), "didn't start listening") {
		t.Errorf("got %v", err)
	}
}
