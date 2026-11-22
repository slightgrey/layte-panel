package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

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
	var restored bool
	for _, c := range d.Runner.(*system.FakeRunner).Cmds {
		restored = restored || strings.Contains(string(c.Stdin), "ALTER USER 'shopuser'@'localhost' IDENTIFIED BY 'oldSecret'")
	}
	if !restored {
		t.Error("old MySQL password not put back")
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
	// Not adopted / wrong site user: the jail refuses before anything changes.
	q := shopPW
	q.Username = "shop"
	if _, err := d.wpDBPassword(ctx, agent.Actor{}, q); err == nil {
		t.Error("accepted the old (non-Layte) user")
	}
}
