//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

// TestDiskQuotaOnRealServer turns on disk quotas (it edits the VM's
// /etc/fstab and remounts) and checks a site user can't write past a hard
// limit, and that usage shows the real numbers.
func TestDiskQuotaOnRealServer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	st, err := e.svc.QuotaStatus(ctx, e.actor)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Supported {
		t.Skipf("quotas not supported here: %s", st.Reason)
	}
	if !st.Enabled {
		if st, err = e.svc.EnableQuotas(ctx, e.actor); err != nil {
			t.Fatal(err)
		}
		if !st.Enabled {
			t.Fatalf("quotas still off: %+v", st)
		}
		if b, _ := os.ReadFile("/etc/fstab"); !strings.Contains(string(b), "usrquota") {
			t.Errorf("fstab without usrquota:\n%s", b)
		}
		if _, err := os.Stat("/etc/fstab.layte-orig"); err != nil {
			t.Errorf("no copy of the original fstab: %v", err)
		}
	}

	versions := phpVersions(t)
	const dom = "quota-it.test"
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
	uid, err := e.st.CreateSiteUser(ctx, "quotait", "x", siteID, 0)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := e.st.UserByID(ctx, uid)
	if err := e.svc.SetAccountLimits(ctx, e.actor, u, store.Limits{DiskMB: 20}); err != nil {
		t.Fatal(err)
	}

	big := "/home/" + user + "/" + dom + "/tmp/big.bin"
	out, err := exec.Command("runuser", "-u", user, "--", "dd", "if=/dev/zero", "of="+big, "bs=1M", "count=30").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "quota") {
		t.Errorf("wrote 30 MB with a 20 MB quota: %v %s", err, out)
	}
	u, _ = e.st.UserByID(ctx, uid)
	usage, err := e.svc.AccountUsage(ctx, e.actor, u)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.DiskExact || usage.DiskLimit != 20<<20 || usage.DiskBytes < 15<<20 || usage.DiskBytes > 21<<20 {
		t.Errorf("usage: %+v", usage.AccountUsage)
	}
	os.Remove(big)
	if err := e.svc.ReleaseAccount(ctx, e.actor, u); err != nil {
		t.Fatal(err)
	}
}
