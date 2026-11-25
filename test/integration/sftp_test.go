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
	"github.com/slightgrey/layte-panel/internal/store"
)

// TestSFTP gives an adopted site in the VM an SFTP login and checks it over
// real SSH (it cleans up after itself). Opt in with
//
//	LAYTE_IT_SFTP=<domain>:<user>:<web root>
//
// Password logins are tested when sshpass is installed.
func TestSFTP(t *testing.T) {
	spec := os.Getenv("LAYTE_IT_SFTP")
	if spec == "" {
		t.Skip("set LAYTE_IT_SFTP=<domain>:<user>:<web root>")
	}
	f := strings.Split(spec, ":")
	if len(f) != 3 {
		t.Fatal("LAYTE_IT_SFTP needs 3 fields")
	}
	domain, user, root := f[0], f[1], f[2]
	e := setup(t)
	ctx := context.Background()
	id, err := e.st.CreateSite(ctx, store.Site{Domain: domain, SystemUser: user, RootPath: root, Source: "imported", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	site, _ := e.st.SiteByID(ctx, id)
	site.Adopted = true
	e.st.UpdateSite(ctx, site)

	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "it@layte", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %s", out)
	}
	pub, _ := os.ReadFile(key + ".pub")
	opts := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR"}
	sftp := func(batch string, extra ...string) (string, error) {
		bf := filepath.Join(dir, "batch")
		os.WriteFile(bf, []byte(batch), 0o600)
		args := append(append(append([]string{}, opts...), extra...), "-b", bf, user+"@127.0.0.1")
		out, err := exec.Command("sftp", args...).CombinedOutput()
		return string(out), err
	}
	withKey := []string{"-i", key, "-o", "BatchMode=yes"}

	disable := func() {
		site, _ := e.st.SiteByID(ctx, id)
		e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: false})
	}
	t.Cleanup(func() { disable(); os.Remove(filepath.Join(root, "layte-sftp-it.txt")) })

	st, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Keys: []string{string(pub)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("on: user %s folder %s warnings %v", st.Username, st.Folder, st.Warnings)

	// Key login, jailed: only the site folder is visible; uploads keep the site user.
	out, err := sftp("pwd\nls /\ncd /"+domain+"\nput /etc/hostname layte-sftp-it.txt\ncd ..\ncd ..\npwd\n", withKey...)
	if err != nil {
		t.Fatalf("sftp with key: %v\n%s", err, out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "Remote working directory: /") {
		t.Errorf("escaped the jail with cd ..:\n%s", out)
	}
	// The jail's root holds only the site folder.
	if ls, err := sftp("ls -1 /\n", withKey...); err != nil || strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ls), "sftp> ls -1 /")) != "/"+domain {
		t.Errorf("the jail shows more than the site folder: %v\n%s", err, ls)
	}
	fi, err := os.Stat(filepath.Join(root, "layte-sftp-it.txt"))
	if err != nil {
		t.Fatalf("upload didn't land in the web root: %v", err)
	}
	if o, _ := exec.Command("stat", "-c", "%U %a", filepath.Join(root, "layte-sftp-it.txt")).Output(); strings.TrimSpace(string(o)) != user+" 644" {
		t.Errorf("uploaded file: %s (size %d)", o, fi.Size())
	}
	if out, err := sftp("ls /etc\n", withKey...); err == nil {
		t.Errorf("reached /etc:\n%s", out)
	}

	// No shell, no port forwarding.
	o, _ := exec.Command("ssh", append(append(append([]string{}, opts...), withKey...), user+"@127.0.0.1", "id")...).CombinedOutput()
	if !strings.Contains(string(o), "sftp connections only") {
		t.Errorf("shell attempt: %s", o)
	}
	// -W asks the server for a TCP forward right away (stdin closed: it ends).
	fwdCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fwd := exec.CommandContext(fwdCtx, "ssh", append(append(append([]string{}, opts...), withKey...), "-W", "127.0.0.1:22", user+"@127.0.0.1")...)
	if o, _ := fwd.CombinedOutput(); !strings.Contains(string(o), "prohibited") && !strings.Contains(string(o), "forwarding failed") {
		t.Errorf("port forwarding not refused: %s", o)
	}

	// Other users keep their settings.
	if o, _ := exec.Command("sshd", "-T", "-C", "user=root,host=x,addr=127.0.0.1").Output(); strings.Contains(string(o), "chrootdirectory /srv") {
		t.Error("root got the SFTP jail")
	}

	// The mount survives a restart of its unit.
	if o, err := exec.Command("systemctl", "restart", "layte-sftp-"+user+".service").CombinedOutput(); err != nil {
		t.Fatalf("restart unit: %s", o)
	}
	if out, err := sftp("ls /"+domain+"/layte-sftp-it.txt\n", withKey...); err != nil {
		t.Errorf("after unit restart: %v\n%s", err, out)
	}

	// Passwords (with sshpass): off → refused; on + generated → works.
	if _, err := exec.LookPath("sshpass"); err == nil {
		// BatchMode=no: sftp -b turns on BatchMode, which would skip the password.
		pwOpts := []string{"-o", "BatchMode=no", "-o", "PreferredAuthentications=password", "-o", "PubkeyAuthentication=no"}
		try := func(pw string) error {
			bf := filepath.Join(dir, "batch")
			os.WriteFile(bf, []byte("pwd\n"), 0o600)
			args := append(append(append([]string{"-p", pw, "sftp"}, opts...), pwOpts...), "-b", bf, user+"@127.0.0.1")
			return exec.Command("sshpass", args...).Run()
		}
		site, _ = e.st.SiteByID(ctx, id)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Password: true, Keys: []string{string(pub)}}); err != nil {
			t.Fatal(err)
		}
		site, _ = e.st.SiteByID(ctx, id)
		pw, err := e.svc.NewSFTPPassword(ctx, e.actor, site)
		if err != nil {
			t.Fatal(err)
		}
		if err := try(pw); err != nil {
			t.Errorf("password login failed: %v", err)
		}
		if err := try(pw + "x"); err == nil {
			t.Error("wrong password accepted")
		}
		site, _ = e.st.SiteByID(ctx, id)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Password: false, Keys: []string{string(pub)}}); err != nil {
			t.Fatal(err)
		}
		if err := try(pw); err == nil {
			t.Error("password still works after password logins were turned off")
		}
		o, _ := exec.Command("ssh", append(append(append([]string{}, opts...), withKey...), user+"@127.0.0.1", "id")...).CombinedOutput()
		if !strings.Contains(string(o), "sftp connections only") {
			t.Errorf("shell attempt after password changes: %s", o)
		}
	} else {
		t.Log("sshpass not installed: password logins not tested")
	}

	// Off: login refused, unit and jail gone, the site's files untouched.
	disable()
	if out, err := sftp("pwd\n", withKey...); err == nil {
		t.Errorf("login still works after turning SFTP off:\n%s", out)
	}
	for _, p := range []string{"/etc/systemd/system/layte-sftp-" + user + ".service", "/srv/layte-sftp/" + user, "/etc/ssh/layte-sftp/" + user} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "layte-sftp-it.txt")); err != nil {
		t.Error("turning SFTP off touched the site's files")
	}
}

// TestSFTPReboot checks that the jail's bind mount comes back after a reboot,
// in two runs around a reboot of the VM:
//
//	LAYTE_IT_SFTP=… LAYTE_IT_SFTP_REBOOT=before  → turns SFTP on, keeps it
//	(reboot)
//	LAYTE_IT_SFTP=… LAYTE_IT_SFTP_REBOOT=after   → logs in, turns SFTP off
func TestSFTPReboot(t *testing.T) {
	phase, spec := os.Getenv("LAYTE_IT_SFTP_REBOOT"), os.Getenv("LAYTE_IT_SFTP")
	if phase == "" || spec == "" {
		t.Skip("set LAYTE_IT_SFTP and LAYTE_IT_SFTP_REBOOT=before|after")
	}
	f := strings.Split(spec, ":")
	domain, user, root := f[0], f[1], f[2]
	e := setup(t)
	ctx := context.Background()
	const key = "/root/layte-it-sftp-key"
	id, _ := e.st.CreateSite(ctx, store.Site{Domain: domain, SystemUser: user, RootPath: root, Source: "imported", Status: "active"})
	site, _ := e.st.SiteByID(ctx, id)
	site.Adopted = true
	switch phase {
	case "before":
		os.Remove(key)
		os.Remove(key + ".pub")
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen: %s", out)
		}
		pub, _ := os.ReadFile(key + ".pub")
		e.st.UpdateSite(ctx, site)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: true, Keys: []string{string(pub)}}); err != nil {
			t.Fatal(err)
		}
		t.Log("SFTP on; reboot, then run with LAYTE_IT_SFTP_REBOOT=after")
	case "after":
		bf := filepath.Join(t.TempDir(), "b")
		os.WriteFile(bf, []byte("ls -1 /"+domain+"\n"), 0o600)
		out, err := exec.Command("sftp", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
			"-i", key, "-b", bf, user+"@127.0.0.1").CombinedOutput()
		if err != nil || !strings.Contains(string(out), "index.php") {
			t.Errorf("after reboot: %v\n%s", err, out)
		}
		site.SFTP.Enabled = true
		e.st.UpdateSite(ctx, site)
		if _, err := e.svc.SetSFTP(ctx, e.actor, site, sites.SFTPRequest{Enabled: false}); err != nil {
			t.Fatal(err)
		}
		os.Remove(key)
		os.Remove(key + ".pub")
	}
}
