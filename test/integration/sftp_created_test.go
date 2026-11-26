//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/sites"
)

// TestSFTPCreatedSite gives a site Layte creates an SFTP login and checks it
// over real SSH: key login jailed to the site folder, uploads owned by the
// site user, no shell, password login only while allowed (with sshpass).
// Self-contained: it creates and deletes its own site, so it runs on any VM.
func TestSFTPCreatedSite(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	const dom = "sftp-it.test"
	versions := phpVersions(t)
	jobID, siteID, err := e.svc.Create(ctx, e.actor, sites.CreateRequest{Domain: dom, PHPVersion: versions[len(versions)-1]})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	site, _ := e.st.SiteByID(ctx, siteID)
	user := site.SystemUser
	t.Cleanup(func() {
		site, _ := e.st.SiteByID(ctx, siteID)
		if j, err := e.svc.Delete(ctx, e.actor, site, sites.DeleteRequest{ConfirmDomain: dom, RemoveFiles: true}); err == nil {
			e.wait(t, j)
		}
	})

	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %s", out)
	}
	pub, _ := os.ReadFile(key + ".pub")
	if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Keys: []string{string(pub)}}); err != nil {
		t.Fatal(err)
	}

	opts := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR"}
	sftp := func(batch string, extra ...string) (string, error) {
		bf := filepath.Join(dir, "batch")
		os.WriteFile(bf, []byte(batch), 0o600)
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		args := append(append(append([]string{}, opts...), extra...), "-b", bf, user+"@127.0.0.1")
		out, err := exec.CommandContext(ctx, "sftp", args...).CombinedOutput()
		return string(out), err
	}
	withKey := []string{"-i", key, "-o", "BatchMode=yes"}

	out, err := sftp("put /etc/hostname /"+dom+"/public/sftp-it.txt\ncd /\ncd ..\npwd\n", withKey...)
	if err != nil {
		t.Fatalf("sftp with key: %v\n%s", err, out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "Remote working directory: /") {
		t.Errorf("escaped the jail:\n%s", out)
	}
	if ls, err := sftp("ls -1 /\n", withKey...); err != nil || strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ls), "sftp> ls -1 /")) != "/"+dom {
		t.Errorf("the jail shows more than the site folder: %v\n%s", err, ls)
	}
	uploaded := "/home/" + user + "/" + dom + "/public/sftp-it.txt"
	if o, _ := exec.Command("stat", "-c", "%U %a", uploaded).Output(); strings.TrimSpace(string(o)) != user+" 644" {
		t.Errorf("uploaded file: %q", o)
	}
	o, _ := exec.Command("ssh", append(append(append([]string{}, opts...), withKey...), user+"@127.0.0.1", "id")...).CombinedOutput()
	if !strings.Contains(string(o), "sftp connections only") {
		t.Errorf("shell attempt: %s", o)
	}

	if _, err := exec.LookPath("sshpass"); err == nil {
		try := func(pw string) error {
			bf := filepath.Join(dir, "batch")
			os.WriteFile(bf, []byte("pwd\n"), 0o600)
			args := append(append([]string{"-p", pw, "sftp"}, opts...), "-o", "BatchMode=no", "-o", "PreferredAuthentications=password", "-o", "PubkeyAuthentication=no", "-b", bf, user+"@127.0.0.1")
			return exec.Command("sshpass", args...).Run()
		}
		site, _ = e.st.SiteByID(ctx, siteID)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Password: true, Keys: []string{string(pub)}}); err != nil {
			t.Fatal(err)
		}
		site, _ = e.st.SiteByID(ctx, siteID)
		pw, err := e.svc.NewSFTPPassword(ctx, e.actor, site)
		if err != nil {
			t.Fatal(err)
		}
		if err := try(pw); err != nil {
			t.Errorf("password login failed: %v", err)
		}
		site, _ = e.st.SiteByID(ctx, siteID)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Keys: []string{string(pub)}}); err != nil {
			t.Fatal(err)
		}
		if err := try(pw); err == nil {
			t.Error("password still works after password logins were turned off")
		}
	} else {
		t.Log("sshpass not installed: password logins not tested")
	}

	site, _ = e.st.SiteByID(ctx, siteID)
	if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if out, err := sftp("pwd\n", withKey...); err == nil {
		t.Errorf("login still works after turning SFTP off:\n%s", out)
	}
}
