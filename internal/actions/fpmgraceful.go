package actions

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"time"

	"github.com/slightgrey/layte-panel/internal/system"
)

// Graceful PHP-FPM reloads. Ubuntu's php-fpm.conf leaves
// process_control_timeout at 0, so on every reload (Layte reloads PHP-FPM
// whenever it changes a pool) the master kills running PHP processes and
// visitors whose request was in progress get a 502. With a timeout, the
// processes first finish their current request.
//
// php-fpm.conf is the admin's, so Layte doesn't edit it: it adds a small
// file of its own to pool.d/ (php-fpm.conf includes pool.d/*.conf, and a
// [global] section there sets global options). If php-fpm.conf already
// sets process_control_timeout, that is left alone.

// fpmGracefulTimeout bounds how long a reload waits for running requests.
const fpmGracefulTimeout = "10s"

const fpmGraceful = system.MarkerINI + ` — do not edit by hand
; Reloads let running PHP requests finish (up to ` + fpmGracefulTimeout + `) instead of
; cutting them off. Remove this file to go back to PHP-FPM's default.
[global]
process_control_timeout = ` + fpmGracefulTimeout + `
`

func fpmGracefulPath(v string) string { return "/etc/php/" + v + "/fpm/pool.d/00-layte-graceful.conf" }

// adminTimeout matches an active process_control_timeout line.
var adminTimeout = regexp.MustCompile(`(?m)^[ \t]*process_control_timeout[ \t]*=`)

// ensureFPMGraceful installs the file for one PHP version (tested with
// php-fpm -t). It takes effect at PHP-FPM's next reload or restart. It
// returns false when the admin's php-fpm.conf sets the timeout itself.
func (d Deps) ensureFPMGraceful(ctx context.Context, v string) (bool, error) {
	if b, err := d.FS.ReadFile("/etc/php/" + v + "/fpm/php-fpm.conf"); err == nil && adminTimeout.Match(b) {
		return false, nil
	}
	err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{Path: fpmGracefulPath(v), Content: []byte(fpmGraceful), Test: fpmTest(v)})
	if err != nil {
		return false, configError(err)
	}
	return true, nil
}

// EnsureFPMGraceful does it for every installed PHP version (agent start).
func (d Deps) EnsureFPMGraceful(ctx context.Context, log *slog.Logger) {
	versions, err := d.phpVersions()
	if err != nil {
		log.Warn("graceful PHP-FPM reloads: can't list PHP versions", "err", err)
		return
	}
	for _, v := range versions {
		if ok, err := d.ensureFPMGraceful(ctx, v); err != nil {
			log.Warn("graceful PHP-FPM reloads", "php", v, "err", err)
		} else if !ok {
			log.Info("graceful PHP-FPM reloads: php-fpm.conf sets process_control_timeout itself; left alone", "php", v)
		}
	}
}

// fpmSocketWait bounds the wait for a new pool's socket: a reload first lets
// running requests finish (fpmGracefulTimeout), then starts the pools.
const fpmSocketWait = 30 * time.Second

// waitFPMSocket waits until a pool's socket accepts connections, so nginx is
// only pointed at it once PHP-FPM really serves it. With graceful reloads
// the new pool can start seconds after `systemctl reload` returns.
func (d Deps) waitFPMSocket(ctx context.Context, socket string) error {
	if d.WaitSocket == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, fpmSocketWait)
	defer cancel()
	if err := d.WaitSocket(ctx, socket); err != nil {
		return fmt.Errorf("PHP-FPM didn't start listening on %s: %w", socket, err)
	}
	return nil
}

// waitUnixSocket dials the socket until it answers or ctx ends.
func waitUnixSocket(ctx context.Context, path string) error {
	var d net.Dialer
	for {
		c, err := d.DialContext(ctx, "unix", path)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(100 * time.Millisecond):
		}
	}
}
