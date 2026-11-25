package actions

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGhV5zFqYBsCm/yHg5lX2yNSv3SrUdKjSRaXzt1TZdQt vince@pc"

func TestNormalizeSSHKey(t *testing.T) {
	n, fp, err := NormalizeSSHKey("  " + testKey + "  ")
	if err != nil || n != testKey || !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("%q %q %v", n, fp, err)
	}
	for _, bad := range []string{
		"",
		"hello",
		`command="rm -rf /" ` + testKey,
		testKey + "\n" + testKey,
		"ssh-dss AAAAB3NzaC1kc3MAAACBAP== x",
	} {
		if _, _, err := NormalizeSSHKey(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// The comment is only a label: control characters are dropped.
	if n, _, _ := NormalizeSSHKey(testKey + "\x07bell"); strings.ContainsRune(n, 7) {
		t.Errorf("control character kept: %q", n)
	}
}

func TestSFTPSetAndDisable(t *testing.T) {
	d := devDeps(t)
	if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.siteDirsCreate(ctx, agent.Actor{}, DirsParams{Domain: "a.test", Username: "acme"}); err != nil {
		t.Fatal(err)
	}
	p := SFTPParams{Domain: "a.test", Username: "acme", Keys: []string{testKey}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := d.sftpSet(ctx, agent.Actor{}, p)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(SFTPResult); r.Username != "acme" || r.Folder != "/a.test" {
		t.Errorf("result: %+v", r)
	}
	conf, _ := d.FS.ReadFile(sftpConfigPath)
	if !system.IsManaged(conf) || strings.Index(string(conf), "Match Group layte-sftp-password") > strings.Index(string(conf), "Match Group layte-sftp\n") ||
		!strings.Contains(string(conf), "ForceCommand internal-sftp -u 0022") || !strings.Contains(string(conf), "ChrootDirectory /srv/layte-sftp/%u") {
		t.Errorf("sshd drop-in:\n%s", conf)
	}
	if k, _ := d.FS.ReadFile(sftpKeysDir + "/acme"); string(k) != testKey+"\n" {
		t.Errorf("keys file: %q", k)
	}
	unit, _ := d.FS.ReadFile(systemdUnitDir + "/" + sftpUnit("acme"))
	if !strings.Contains(string(unit), "ExecStart=/usr/bin/mount --bind /home/acme/a.test /srv/layte-sftp/acme/a.test\n") ||
		!strings.Contains(string(unit), "ConditionPathIsMountPoint=!/srv/layte-sftp/acme/a.test\n") {
		t.Errorf("mount unit:\n%s", unit)
	}
	calls := strings.Join(d.Runner.(*system.FakeRunner).CallList(), "\n")
	for _, want := range []string{"/usr/sbin/sshd -t", "/usr/bin/systemctl reload ssh", "enable --now " + sftpUnit("acme"), "--lock acme"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q in:\n%s", want, calls)
		}
	}

	// No password option: no password can be generated.
	if _, err := d.sftpPassword(ctx, agent.Actor{}, p); err == nil {
		t.Error("password generated while password logins are off")
	}
	// With it: a fresh password, set via stdin.
	p.Password = true
	if _, err := d.sftpSet(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	pw, err := d.sftpPassword(ctx, agent.Actor{}, p)
	if err != nil {
		t.Fatal(err)
	}
	if s := pw.(SFTPPassword).Password; len(s) != 24 || strings.Contains(strings.Join(d.Runner.(*system.FakeRunner).CallList(), " "), s) {
		t.Errorf("password %q (must not appear in command lines)", s)
	}

	// Off: keys, unit and jail gone.
	if _, err := d.sftpDisable(ctx, agent.Actor{}, p); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{sftpKeysDir + "/acme", systemdUnitDir + "/" + sftpUnit("acme"), sftpJail("acme")} {
		if ok, _ := d.FS.Exists(f); ok {
			t.Errorf("%s left behind", f)
		}
	}
	if _, err := d.sftpPassword(ctx, agent.Actor{}, p); err == nil {
		t.Error("password generated after SFTP was turned off")
	}
}

func TestSFTPRefusals(t *testing.T) {
	d := devDeps(t)
	for _, bad := range []SFTPParams{
		{Domain: "a.test"},
		{Domain: "a.test", Username: "root"},
		{Domain: "a.test", Username: "acme", WebRoot: "/etc"},
		{Domain: "a.test", Username: "acme", Keys: []string{"nope"}},
		{Domain: "a.test", Username: "acme", Keys: []string{"  " + testKey}}, // not normalised
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// Not a Layte site user (the imported blog's owner): refused.
	if _, err := d.sftpSet(ctx, agent.Actor{}, SFTPParams{Domain: "shop.example.org", Username: "shop", WebRoot: "/var/www/shop/public"}); err == nil {
		t.Error("SFTP for a user Layte didn't create")
	}
	// Adopted site: the web root goes into the jail.
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sftpSet(ctx, agent.Actor{}, SFTPParams{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/shop/public"}); err != nil {
		t.Fatal(err)
	}
	unit, _ := d.FS.ReadFile(systemdUnitDir + "/" + sftpUnit("shopexampleorg"))
	if !strings.Contains(string(unit), "--bind /var/www/shop/public /srv/layte-sftp/shopexampleorg/shop.example.org\n") {
		t.Errorf("mount unit:\n%s", unit)
	}
}
