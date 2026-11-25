package actions

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/nginx"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
)

const (
	shopVhost      = "/etc/nginx/sites-enabled/shop.example.org.conf"
	shopKept       = "/etc/nginx/sites-available/shop.example.org.conf.layte-orig"
	shopLayteVhost = "/etc/nginx/sites-available/layte-shop.example.org.conf"
	shopLayteLink  = "/etc/nginx/sites-enabled/layte-shop.example.org.conf"
)

var shopTakeover = TakeoverParams{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/shop/public"}

func takeoverShop(t *testing.T, d Deps) (any, error) {
	t.Helper()
	out, err := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover)
	if err != nil {
		t.Fatal(err)
	}
	p := shopTakeover
	p.VhostHash = out.(TakeoverPreview).VhostHash
	return d.siteNginxTakeover(ctx, agent.Actor{}, p)
}

func TestTakeoverPreview(t *testing.T) {
	d := devDeps(t)
	// Not adopted yet: refused.
	if _, err := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover); err == nil {
		t.Error("previewed a site that isn't adopted")
	}
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	out, err := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover)
	if err != nil {
		t.Fatal(err)
	}
	p := out.(TakeoverPreview)
	if p.VhostFile != shopVhost || p.KeptAs != shopKept || p.NewVhostFile != shopLayteVhost || p.PHPVersion != "8.2" ||
		p.NewPoolFile != "/etc/php/8.2/fpm/pool.d/layte-shop.example.org.conf" || len(p.Blockers) != 0 || p.VhostHash == "" {
		t.Errorf("preview: %+v", p)
	}
	diff := strings.Join(p.VhostDiff, "\n")
	if !strings.Contains(diff, "      root /var/www/shop/public;") || !strings.Contains(diff, "-     access_log /var/log/nginx/shop.access.log;") ||
		!strings.Contains(diff, "+         fastcgi_pass unix:/run/php/layte-shop.example.org-php8.2.sock;") {
		t.Errorf("vhost diff:\n%s", diff)
	}
	if pool := strings.Join(p.PoolDiff, "\n"); !strings.Contains(pool, "+ php_admin_value[open_basedir] = /var/www/shop/public/:/home/shopexampleorg/") {
		t.Errorf("pool diff:\n%s", pool)
	}
}

func TestSiteNginxTakeover(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	orig, _ := d.FS.ReadFile(shopVhost)
	out, err := takeoverShop(t, d)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(TakeoverResult)
	if r.VhostFile != shopLayteVhost || r.KeptAs != shopKept || r.PHPVersion != "8.2" {
		t.Errorf("result: %+v", r)
	}
	if ok, _ := d.FS.Exists(shopVhost); ok {
		t.Error("admin's vhost still enabled")
	}
	if kept, _ := d.FS.ReadFile(shopKept); string(kept) != string(orig) {
		t.Error("admin's vhost not kept")
	}
	vhost, _ := d.FS.ReadFile(shopLayteLink)
	if !system.IsManaged(vhost) || !strings.Contains(string(vhost), "root /var/www/shop/public;") ||
		!strings.Contains(string(vhost), "fastcgi_pass unix:/run/php/layte-shop.example.org-php8.2.sock;") {
		t.Errorf("new vhost:\n%s", vhost)
	}
	if ok, _ := d.FS.Exists(shopPool); ok {
		t.Error("old adopted pool not removed")
	}
	if ok, _ := d.FS.Exists(shopPool + system.AdoptSuffix); !ok {
		t.Error("pre-adoption original pool was removed")
	}

	// Still an adopted site for files/cron, and now config writes are allowed.
	if err := d.verifyAdopted(ctx, "shop.example.org", "shopexampleorg", "/var/www/shop/public"); err != nil {
		t.Errorf("verifyAdopted after takeover: %v", err)
	}
	cfg := SiteConfigParams{Domain: "shop.example.org", Aliases: []string{"www.shop.example.org"}, Username: "shopexampleorg",
		PHPVersion: "8.3", WebRoot: "/var/www/shop/public"}
	if _, err := d.phpPoolWrite(ctx, agent.Actor{}, cfg); err != nil {
		t.Fatalf("pool write after takeover: %v", err)
	}
	if _, err := d.nginxVhostWrite(ctx, agent.Actor{}, cfg); err != nil {
		t.Fatalf("vhost write after takeover: %v", err)
	}
	if b, _ := d.FS.ReadFile(shopLayteVhost); !strings.Contains(string(b), "php8.3.sock") {
		t.Error("PHP switch didn't reach the vhost")
	}
	// Taking over twice: refused.
	if _, err := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover); err == nil {
		t.Error("took over twice")
	}
}

func TestSiteNginxTakeoverRollsBack(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	orig, _ := d.FS.ReadFile(shopVhost)
	pool, _ := d.FS.ReadFile(shopPool)
	calls := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		calls++
		if calls == 1 {
			return 200, nil
		}
		return 404, nil // the admin's vhost had a rule Layte's doesn't
	}
	if _, err := takeoverShop(t, d); err == nil || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("expected rollback, got %v", err)
	}
	if b, _ := d.FS.ReadFile(shopVhost); string(b) != string(orig) {
		t.Error("admin's vhost not restored")
	}
	if b, _ := d.FS.ReadFile(shopPool); string(b) != string(pool) {
		t.Error("adopted pool changed")
	}
	for _, p := range []string{shopKept, shopLayteVhost, shopLayteLink, templates.PoolPath("8.2", "shop.example.org")} {
		if ok, _ := d.FS.Exists(p); ok {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestTakeoverRefusals(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	// Changed since the preview.
	p := shopTakeover
	p.VhostHash = "0000000000000000"
	if _, err := d.siteNginxTakeover(ctx, agent.Actor{}, p); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("got %v", err)
	}
	// HTTPS without a Layte certificate.
	out, _ := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover)
	p.VhostHash, p.SSL = out.(TakeoverPreview).VhostHash, true
	if _, err := d.siteNginxTakeover(ctx, agent.Actor{}, p); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("got %v", err)
	}
	// Config writes before the takeover would add a second vhost: refused.
	cfg := SiteConfigParams{Domain: "shop.example.org", Username: "shopexampleorg", PHPVersion: "8.2", WebRoot: "/var/www/shop/public"}
	if _, err := d.nginxVhostWrite(ctx, agent.Actor{}, cfg); err == nil {
		t.Error("wrote a vhost next to the admin's")
	}
	// A file that also serves another domain is blocked.
	d.FS.WriteFileAtomic(shopVhost, append(mustRead(t, d, shopVhost), []byte("\nserver { listen 80; server_name other.example.org; root /var/www/other; }\n")...), 0o644)
	out, err := d.takeoverPreview(ctx, agent.Actor{}, shopTakeover)
	if err != nil {
		t.Fatal(err)
	}
	if b := out.(TakeoverPreview).Blockers; len(b) == 0 || !strings.Contains(b[0], "other.example.org") {
		t.Errorf("blockers: %v", b)
	}
}

func mustRead(t *testing.T, d Deps, p string) []byte {
	t.Helper()
	b, err := d.FS.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLineDiff(t *testing.T) {
	got := strings.Join(lineDiff("a\nb\nc\n", "a\nx\nc\n"), "|")
	if got != "  a|- b|+ x|  c" {
		t.Errorf("diff = %q", got)
	}
}

func TestTakenOverCacheMarker(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := takeoverShop(t, d); err != nil {
		t.Fatal(err)
	}
	dir := pagecache.Dir("shop.example.org")
	d.FS.MkdirAll(dir+"/a", 0o700)
	d.FS.WriteFileAtomic(dir+"/a/page", []byte("cached"), 0o600)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	marker := "/home/shopexampleorg/tmp/" + pagecache.MarkerName
	d.FS.WriteFileAtomic(marker, nil, 0o644)
	if got := d.purgeMarked(log); len(got) != 1 || got[0] != "shop.example.org" {
		t.Errorf("purged %v", got)
	}
	if ok, _ := d.FS.Exists(dir + "/a/page"); ok {
		t.Error("cache not purged")
	}
}

func TestCustomDirectives(t *testing.T) {
	ds, err := nginx.Parse(`server {
    server_name a.test;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    location / { try_files $uri $uri/ /index.php?$args; }
    location ~ /\. { deny all; }
    location /private { auth_basic "x"; }
    add_header X-Frame-Options SAMEORIGIN;
    include snippets/custom.conf;
}`)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(customDirectives(ds), "|")
	for _, want := range []string{"location /private", "auth_basic x", "add_header X-Frame-Options SAMEORIGIN", "include snippets/custom.conf"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "letsencrypt") || strings.Contains(got, `location ~ /\.`) {
		t.Errorf("template-covered directives reported: %q", got)
	}
}

func TestNginxWorkers(t *testing.T) {
	d := devDeps(t)
	if w := d.nginxWorkers(); len(w) != 0 {
		t.Errorf("no PID file: %v", w)
	}
	d.FS.MkdirAll("/run", 0o755)
	d.FS.WriteFileAtomic("/run/nginx.pid", []byte("5000\n"), 0o644)
	for pid, stat := range map[string]string{
		"5000": "5000 (nginx) S 1 5000",
		"5001": "5001 (nginx) S 5000 5000",
		"5002": "5002 (a (b) c) S 5000 5002", // odd comm, still a child
		"5003": "5003 (php-fpm8.3) S 1 5003",
	} {
		d.FS.MkdirAll("/proc/"+pid, 0o755)
		d.FS.WriteFileAtomic("/proc/"+pid+"/stat", []byte(stat), 0o644)
	}
	w := d.nginxWorkers()
	if len(w) != 2 || !slices.Contains(w, 5001) || !slices.Contains(w, 5002) {
		t.Errorf("workers = %v", w)
	}
	d.FS.RemoveAll("/proc/5001")
	d.FS.RemoveAll("/proc/5002")
	if !d.waitNginxReloaded(ctx, w) {
		t.Error("old workers are gone, but still waiting")
	}
}

func TestOwnCertChecks(t *testing.T) {
	for _, bad := range []string{"/etc/letsencrypt/live", "/etc/letsencrypt/live/../x", "/etc/ssl/private", "/etc/letsencrypt/live/a/b"} {
		p := SiteConfigParams{Domain: "shop.example.org", Username: "shopexampleorg", PHPVersion: "8.2", WebRoot: "/var/www/shop/public", OwnCert: bad}
		if p.Validate() == nil {
			t.Errorf("accepted own_cert %s", bad)
		}
	}
	if (SiteConfigParams{Domain: "a.test", Username: "atest", PHPVersion: "8.2", OwnCert: "/etc/letsencrypt/live/a.test"}).Validate() == nil {
		t.Error("own_cert accepted for a site Layte created")
	}
	if got := OwnCertOf("/etc/letsencrypt/live/a.test/fullchain.pem"); got != "/etc/letsencrypt/live/a.test" {
		t.Errorf("OwnCertOf = %q", got)
	}
	if got := OwnCertOf("/etc/ssl/certs/a.pem"); got != "" {
		t.Errorf("OwnCertOf(non-certbot) = %q", got)
	}

	// Taken over without a certificate: pointing at another one is refused.
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := takeoverShop(t, d); err != nil {
		t.Fatal(err)
	}
	d.FS.MkdirAll("/etc/letsencrypt/live/other.example.org", 0o755)
	cfg := SiteConfigParams{Domain: "shop.example.org", Aliases: []string{"www.shop.example.org"}, Username: "shopexampleorg",
		PHPVersion: "8.2", WebRoot: "/var/www/shop/public", SSL: true, OwnCert: "/etc/letsencrypt/live/other.example.org"}
	if _, err := d.nginxVhostWrite(ctx, agent.Actor{}, cfg); err == nil || !strings.Contains(err.Error(), "doesn't use the certificate") {
		t.Errorf("got %v", err)
	}
}
