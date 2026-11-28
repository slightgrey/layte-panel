//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

// TestSiteLoginOnRealServer is a site login end to end on real systemd,
// cgroups, PHP-FPM, nginx, cron and sshd: a main site and a subdomain site on
// one Linux user; the login's limits (its own PHP-FPM in a slice, cron jobs
// in it); PHP shell functions blocked; one SFTP login for both folders;
// deleting the subdomain; refusing a name a hand-written vhost serves; and
// releasing the login. Self-contained: it creates and deletes its own sites.
func TestSiteLoginOnRealServer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	versions := phpVersions(t)
	php := versions[len(versions)-1]
	const mainDom, subDom = "acct-it.test", "blog.acct-it.test"

	jobID, mainID, err := e.svc.Create(ctx, e.actor, sites.CreateRequest{Domain: mainDom, PHPVersion: php})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	main, _ := e.st.SiteByID(ctx, mainID)
	user := main.SystemUser
	t.Cleanup(func() {
		for _, dom := range []string{subDom, mainDom} {
			if s, err := e.st.SiteByDomain(ctx, dom); err == nil {
				if j, err := e.svc.Delete(ctx, e.actor, s, sites.DeleteRequest{ConfirmDomain: dom, RemoveFiles: true}); err == nil {
					e.wait(t, j)
				}
			}
		}
	})

	// The login, with limits: half a CPU, 256 MB.
	uid, err := e.st.CreateSiteUser(ctx, "acctit", "x", mainID, 3)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := e.st.UserByID(ctx, uid)
	if err := e.svc.BlockAccountShell(ctx, e.actor, mainID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetAccountLimits(ctx, e.actor, u, store.Limits{CPUPercent: 50, MemoryMB: 256}); err != nil {
		t.Fatal(err)
	}
	main, _ = e.st.SiteByID(ctx, mainID)
	if !main.OwnFPM || !main.BlockPHPShell {
		t.Fatalf("main site not moved into the login: %+v", main)
	}
	slice := sliceOf(t, user)
	if got := systemctlShow(t, slice, "CPUQuotaPerSecUSec"); got != "500ms" {
		t.Errorf("CPUQuota: %s", got)
	}
	if got := systemctlShow(t, slice, "MemoryMax"); got != strconv.Itoa(256<<20) {
		t.Errorf("MemoryMax: %s", got)
	}
	unit := "layte-fpm-" + user + "-" + php + ".service"
	if out, err := exec.Command("systemctl", "is-active", unit).Output(); err != nil || strings.TrimSpace(string(out)) != "active" {
		t.Fatalf("%s: %s %v", unit, out, err)
	}

	// The site serves from the login's own PHP, inside the slice.
	pub := "/home/" + user + "/" + mainDom + "/public"
	writeAs(t, user, pub+"/it.php", "<?php echo 'php-ok ', getmypid();")
	body := getPath(t, mainDom, "/it.php")
	if !strings.Contains(body, "php-ok ") {
		t.Fatalf("site doesn't run PHP after the move: %s", body)
	}
	pid := strings.TrimSpace(body[strings.LastIndex(body, " ")+1:])
	if cg, _ := os.ReadFile("/proc/" + pid + "/cgroup"); !strings.Contains(string(cg), slice) {
		t.Errorf("PHP worker %s not in %s: %s", pid, slice, cg)
	}
	if _, err := os.Stat(actions.CronJobsPath(mainDom)); err == nil {
		t.Error("cron jobs file without cron jobs")
	}

	// RAM cap: a request that wants 400 MB is stopped by the kernel.
	writeAs(t, user, pub+"/big.php", "<?php ini_set('memory_limit', '-1'); $a = str_repeat('x', 400 * 1024 * 1024); echo 'got ', strlen($a);")
	if body := getPath(t, mainDom, "/big.php"); strings.Contains(body, "got ") {
		t.Errorf("a 400 MB request ran in a 256 MB limit: %.200s", body)
	}
	if ev, _ := os.ReadFile("/sys/fs/cgroup/layte.slice/layte-acct.slice/" + slice + "/memory.events"); !strings.Contains(string(ev), "oom_kill ") || strings.Contains(string(ev), "oom_kill 0") {
		t.Errorf("no OOM kill in the slice: %s", ev)
	}
	if body := getPath(t, mainDom, "/it.php"); !strings.Contains(body, "php-ok ") {
		t.Errorf("site down after the OOM kill: %.200s", body)
	}

	// PHP shell functions: blocked, then allowed by the admin.
	writeAs(t, user, pub+"/sh.php", "<?php echo function_exists('exec') && @exec('id -un') ? 'shell-on' : 'shell-off';")
	if body := getPath(t, mainDom, "/sh.php"); !strings.Contains(body, "shell-off") {
		t.Errorf("exec works while blocked: %.200s", body)
	}
	main, _ = e.st.SiteByID(ctx, mainID)
	if err := e.svc.SetPHPShell(ctx, e.actor, main, false); err != nil {
		t.Fatal(err)
	}
	if body := getPath(t, mainDom, "/sh.php"); !strings.Contains(body, "shell-on") {
		t.Errorf("exec still blocked after allowing it: %.200s", body)
	}

	// A subdomain site: same Linux user, the login's PHP and limits.
	jobID, subID, err := e.svc.Create(ctx, e.actor, sites.CreateRequest{Domain: subDom, PHPVersion: php, ParentSiteID: mainID})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	sub, _ := e.st.SiteByID(ctx, subID)
	if sub.SystemUser != user || !sub.OwnFPM {
		t.Fatalf("subdomain site: %+v", sub)
	}
	writeAs(t, user, "/home/"+user+"/"+subDom+"/public/it.php", "<?php echo 'sub-ok';")
	// A new server name is live once nginx's reload has handed over (a moment).
	body = ""
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !strings.Contains(body, "sub-ok"); time.Sleep(250 * time.Millisecond) {
		body = getPath(t, subDom, "/it.php")
	}
	if !strings.Contains(body, "sub-ok") {
		dbg, _ := exec.Command("sh", "-c", "nginx -T 2>/dev/null | grep -n -A3 'server_name "+subDom+"'; ls -la /home/"+user+"/"+subDom+"/public /etc/nginx/sites-enabled | head -30; tail -5 /var/log/nginx/error.log").CombinedOutput()
		t.Errorf("subdomain doesn't serve: %.200s\n%s", body, dbg)
	}

	// Cron inside the slice, as the site's user (run the line's command now).
	main, _ = e.st.SiteByID(ctx, mainID)
	tmp := "/home/" + user + "/" + mainDom + "/tmp"
	if _, err := e.svc.AddCron(ctx, e.actor, main, "0 3 * * *", "id -un > "+tmp+"/who.txt; cat /proc/self/cgroup > "+tmp+"/cg.txt"); err != nil {
		t.Fatal(err)
	}
	line, _ := os.ReadFile(actions.CronPath(mainDom))
	if !strings.Contains(string(line), " root /usr/local/bin/layte cron-exec "+mainDom+" 0") || strings.Contains(string(line), "who.txt") {
		t.Errorf("cron line:\n%s", line)
	}
	if out, err := exec.Command("/usr/local/bin/layte", "cron-exec", mainDom, "0").CombinedOutput(); err != nil {
		t.Fatalf("cron-exec: %v %s", err, out)
	}
	if who, _ := os.ReadFile(tmp + "/who.txt"); strings.TrimSpace(string(who)) != user {
		t.Errorf("cron job ran as %q", who)
	}
	if cg, _ := os.ReadFile(tmp + "/cg.txt"); !strings.Contains(string(cg), slice) {
		t.Errorf("cron job not in %s: %s", slice, cg)
	}

	// One SFTP login, both folders.
	key := filepath.Join(t.TempDir(), "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %s", out)
	}
	pubKey, _ := os.ReadFile(key + ".pub")
	main, _ = e.st.SiteByID(ctx, mainID)
	if _, err := e.svc.SetSFTP(ctx, e.actor, main, sites.SFTPRequest{Enabled: true, Keys: []string{string(pubKey)}}); err != nil {
		t.Fatal(err)
	}
	ls := sftpLs(t, key, user)
	if !strings.Contains(ls, "/"+mainDom) || !strings.Contains(ls, "/"+subDom) {
		t.Errorf("SFTP login doesn't show both folders:\n%s", ls)
	}

	// Deleting the subdomain: its folder and mount go, the user and main site stay.
	sub, _ = e.st.SiteByID(ctx, subID)
	j, err := e.svc.Delete(ctx, e.actor, sub, sites.DeleteRequest{ConfirmDomain: subDom, RemoveFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, j)
	if _, err := os.Stat("/home/" + user + "/" + subDom); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("subdomain folder left: %v", err)
	}
	if body := getPath(t, mainDom, "/it.php"); !strings.Contains(body, "php-ok ") {
		t.Errorf("main site broken by deleting the subdomain: %.200s", body)
	}
	if ls := sftpLs(t, key, user); strings.Contains(ls, subDom) || !strings.Contains(ls, mainDom) {
		t.Errorf("SFTP folders after deleting the subdomain:\n%s", ls)
	}

	// A name a hand-written vhost serves can't be taken.
	hand := "/etc/nginx/conf.d/it-hand-written.conf"
	os.WriteFile(hand, []byte("server {\n  listen 80;\n  server_name hand.acct-it.test;\n  return 204;\n}\n"), 0o644)
	t.Cleanup(func() { os.Remove(hand) })
	if _, _, err := e.svc.Create(ctx, e.actor, sites.CreateRequest{Domain: "hand.acct-it.test", PHPVersion: php, ParentSiteID: mainID}); err == nil || !strings.Contains(err.Error(), "already served") {
		t.Errorf("took a hand-written vhost's name: %v", err)
	}

	// Releasing the login: PHP back on the shared service, no slice.
	u, _ = e.st.UserByID(ctx, uid)
	if err := e.svc.ReleaseAccount(ctx, e.actor, u); err != nil {
		t.Fatal(err)
	}
	if body := getPath(t, mainDom, "/it.php"); !strings.Contains(body, "php-ok ") {
		t.Errorf("site down after releasing the login: %.200s", body)
	}
	if _, err := os.Stat("/etc/systemd/system/" + slice); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("slice left after releasing the login: %v", err)
	}
	nginxTestOK(t)
}

// getPath is get for a path other than /.
func getPath(t *testing.T, host, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://127.0.0.1"+path, nil)
	req.Host = host
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.Status + " " + string(b)
}

func sliceOf(t *testing.T, user string) string {
	t.Helper()
	out, err := exec.Command("id", "-u", user).Output()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	return actions.AccountSlice(uint32(uid))
}

func systemctlShow(t *testing.T, unit, prop string) string {
	t.Helper()
	out, err := exec.Command("systemctl", "show", "--value", "--property", prop, unit).Output()
	if err != nil {
		t.Fatalf("systemctl show %s: %v", unit, err)
	}
	return strings.TrimSpace(string(out))
}

// writeAs writes a file as the site user (like an upload would).
func writeAs(t *testing.T, user, path, content string) {
	t.Helper()
	cmd := exec.Command("runuser", "-u", user, "--", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write %s as %s: %v %s", path, user, err, out)
	}
}

func sftpLs(t *testing.T, key, user string) string {
	t.Helper()
	bf := filepath.Join(t.TempDir(), "batch")
	os.WriteFile(bf, []byte("ls -1 /\n"), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sftp", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
		"-i", key, "-o", "BatchMode=yes", "-b", bf, user+"@127.0.0.1").CombinedOutput()
	if err != nil {
		t.Fatalf("sftp: %v\n%s", err, out)
	}
	return string(out)
}
