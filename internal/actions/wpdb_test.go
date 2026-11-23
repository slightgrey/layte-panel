package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func init() { siteSettle = time.Millisecond }

// mysqlSQL is every SQL statement sent to mysql, in order.
func mysqlSQL(d Deps) []string {
	var out []string
	for _, c := range d.Runner.(*system.FakeRunner).Cmds {
		if c.Bin == mysqlBin && len(c.Stdin) > 0 {
			out = append(out, strings.TrimSpace(string(c.Stdin)))
		}
	}
	return out
}

func indexOf(calls []string, sub string) int {
	for i, c := range calls {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}

func TestWPPassword(t *testing.T) {
	src := "<?php\ndefine( 'DB_NAME', 'wp' );\ndefine( 'DB_PASSWORD', 'old-pass' );\n"
	old, replace, ok := wpPassword(src)
	if !ok || old != "old-pass" {
		t.Fatalf("old=%q ok=%v", old, ok)
	}
	if got := replace("NEW"); got != "<?php\ndefine( 'DB_NAME', 'wp' );\ndefine( 'DB_PASSWORD', 'NEW' );\n" {
		t.Errorf("replaced:\n%s", got)
	}
	if old, _, ok := wpPassword(`define("DB_PASSWORD", "dq'pass");`); !ok || old != "dq'pass" {
		t.Errorf("double quotes: %q %v", old, ok)
	}
	for _, bad := range []string{
		`define('DB_PASSWORD', 'esc\'aped');`,                     // escapes
		`define("DB_PASSWORD", "$var");`,                          // PHP interpolation
		`define('DB_PASSWORD', getenv('X'));`,                     // not a literal
		`define('DB_PASSWORD', 'a'); define('DB_PASSWORD', 'b');`, // ambiguous
		`<?php // no password here`,                               // missing
	} {
		if _, _, ok := wpPassword(bad); ok {
			t.Errorf("accepted %s", bad)
		}
	}
	if got := sqlString(`it's\x`); got != `it''s\\x` {
		t.Errorf("sqlString: %s", got)
	}
}

// adoptedWPShop adopts the shop fixture and gives it a WordPress wp-config.php.
func adoptedWPShop(t *testing.T, d Deps) {
	t.Helper()
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	d.FS.WriteFileAtomic("/var/www/shop/public/wp-config.php",
		[]byte("<?php\ndefine( 'DB_NAME', 'shopdb' );\ndefine( 'DB_USER', 'shopuser' );\ndefine( 'DB_PASSWORD', 'oldSecret' );\n"), 0o640)
}

var shopPW = WPDBPasswordParams{Database: "shopdb", User: "shopuser", Domain: "shop.example.org", Username: "shopexampleorg",
	WebRoot: "/var/www/shop/public", Password: "NEWPASSWORD234567ABCDEFGH234567"}

func TestWPDBPasswordRefusesSharedUser(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	inner := d.Runner.(*system.FakeRunner).Handle
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		if strings.HasPrefix(string(c.Stdin), "SHOW GRANTS") {
			return system.Result{Stdout: []byte("GRANT USAGE ON *.* TO `shopuser`@`localhost`\nGRANT ALL PRIVILEGES ON `shopdb`.* TO `shopuser`@`localhost`\nGRANT SELECT ON `otherdb`.* TO `shopuser`@`localhost`\n")}, nil
		}
		return inner(c)
	}}
	_, err := d.wpDBPassword(ctx, agent.Actor{}, shopPW)
	if err == nil || !strings.Contains(err.Error(), "other rights") {
		t.Fatalf("got %v", err)
	}
	for _, c := range d.Runner.(*system.FakeRunner).Cmds {
		if strings.Contains(string(c.Stdin), "ALTER USER") {
			t.Error("password changed despite the shared user")
		}
	}
	// Wrong database or user for this wp-config.php: refused too.
	for _, p := range []WPDBPasswordParams{{Database: "other"}, {User: "other"}} {
		q := shopPW
		if p.Database != "" {
			q.Database = p.Database
		}
		if p.User != "" {
			q.User = p.User
		}
		if _, err := d.wpDBPassword(ctx, agent.Actor{}, q); err == nil {
			t.Errorf("accepted %s/%s", q.Database, q.User)
		}
	}
}

func TestWPDBPasswordRollsBack(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	before, _ := d.FS.ReadFile("/var/www/shop/public/wp-config.php")
	calls := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		calls++
		if calls == 1 {
			return 200, nil
		}
		return 500, nil // "Error establishing a database connection"
	}
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, shopPW); err == nil || !strings.Contains(err.Error(), "put back") {
		t.Fatalf("expected rollback, got %v", err)
	}
	if after, _ := d.FS.ReadFile("/var/www/shop/public/wp-config.php"); string(after) != string(before) {
		t.Errorf("wp-config.php not restored:\n%s", after)
	}
	sql := mysqlSQL(d)
	back := indexOf(sql, "IDENTIFIED BY 'oldSecret';")
	if back < 0 {
		t.Fatalf("old MySQL password not put back: %q", sql)
	}
	if indexOf(sql[back:], "DISCARD OLD PASSWORD") < 0 {
		t.Error("the retained second password wasn't discarded after the rollback")
	}
	reloads := 0
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		if c == "/usr/bin/systemctl reload php8.2-fpm" {
			reloads++
		}
	}
	if reloads < 2 {
		t.Errorf("PHP-FPM reloaded %d times; want after the change and after the rollback", reloads)
	}
}

func TestWPDBPasswordChanges(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, shopPW); err != nil {
		t.Fatal(err)
	}
	b, _ := d.FS.ReadFile("/var/www/shop/public/wp-config.php")
	if !strings.Contains(string(b), "define( 'DB_PASSWORD', '"+shopPW.Password+"' );") {
		t.Errorf("wp-config.php:\n%s", b)
	}
	// MySQL keeps the old password valid until PHP has reloaded the new
	// wp-config.php; only then is the old one dropped.
	calls := d.Runner.(*system.FakeRunner).CallList()
	sql := mysqlSQL(d)
	if indexOf(sql, "IDENTIFIED BY '"+shopPW.Password+"' RETAIN CURRENT PASSWORD;") < 0 {
		t.Errorf("new password not set with RETAIN CURRENT PASSWORD: %q", sql)
	}
	reload := indexOf(calls, "/usr/bin/systemctl reload php8.2-fpm")
	if reload < 0 {
		t.Fatalf("PHP-FPM not reloaded: %q", calls)
	}
	var discardAt int = -1
	for i, c := range d.Runner.(*system.FakeRunner).Cmds {
		if strings.Contains(string(c.Stdin), "DISCARD OLD PASSWORD") {
			discardAt = i
		}
	}
	if discardAt < reload {
		t.Errorf("old password discarded (call %d) before the PHP reload (call %d)", discardAt, reload)
	}
	// Not adopted / wrong site user: the jail refuses before anything changes.
	q := shopPW
	q.Username = "shop"
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, q); err == nil {
		t.Error("accepted the old (non-Layte) user")
	}
}

// Right after the PHP-FPM reload a request can fail once; that is not a failure.
func TestWPDBPasswordWaitsForPHP(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	calls := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		calls++
		if calls == 2 {
			return 502, nil // workers restarting
		}
		return 200, nil
	}
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, shopPW); err != nil {
		t.Fatalf("a brief 502 during the reload must not roll back: %v", err)
	}
	if calls < 3 {
		t.Errorf("site checked %d times; want a retry", calls)
	}
}

// MariaDB has no dual passwords: plain ALTER USER, no DISCARD.
func TestWPDBPasswordMariaDB(t *testing.T) {
	d := devDeps(t)
	adoptedWPShop(t, d)
	inner := d.Runner.(*system.FakeRunner).Handle
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		if c.String() == mysqlBin+" --version" {
			return system.Result{Stdout: []byte("mysql  Ver 15.1 Distrib 10.11.6-MariaDB, for debian-linux-gnu (x86_64)\n")}, nil
		}
		return inner(c)
	}}
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, shopPW); err != nil {
		t.Fatal(err)
	}
	for _, q := range mysqlSQL(d) {
		if strings.Contains(q, "RETAIN") || strings.Contains(q, "DISCARD") {
			t.Errorf("MariaDB got %q", q)
		}
	}
}
