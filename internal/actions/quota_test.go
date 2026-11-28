package actions

import (
	"strings"
	"testing"
)

func TestAddUsrquota(t *testing.T) {
	fstab := "# comment / ext4\nLABEL=root\t/\text4\tdiscard,errors=remount-ro\t0 1\n/dev/sdb1 /srv xfs defaults 0 2\n"
	got, changed, err := addUsrquota(fstab, "/")
	if err != nil || !changed || !strings.Contains(got, "discard,errors=remount-ro,usrquota") || !strings.Contains(got, "/dev/sdb1 /srv xfs defaults 0 2") || !strings.HasPrefix(got, "# comment / ext4\n") {
		t.Fatalf("got %q, %v, %v", got, changed, err)
	}
	if _, changed, _ := addUsrquota(got, "/"); changed {
		t.Error("added usrquota twice")
	}
	if _, _, err := addUsrquota(fstab, "/home"); err == nil {
		t.Error("no line for /home, but no error")
	}
	if _, _, err := addUsrquota(fstab+"LABEL=x / ext4 defaults 0 1\n", "/"); err == nil {
		t.Error("two lines for /, but no error")
	}
}

func TestParseQuota(t *testing.T) {
	out := "Disk quotas for user acme (uid 1003):\n     Filesystem  blocks   quota   limit   grace   files   quota   limit   grace\n      /dev/sda1   20480*  19456   20480  604800       5       0       0       0\n"
	if q, ok := parseQuota(out); !ok || q.Used != 20480*1024 || q.Limit != 20480*1024 {
		t.Errorf("parseQuota = %+v, %v", q, ok)
	}
	if q, ok := parseQuota("Disk quotas for user acme (uid 1003): none\n"); !ok || q != (QuotaUsage{}) {
		t.Errorf("no quota: %+v, %v", q, ok)
	}
}

func TestRemoveUsrquota(t *testing.T) {
	fstab := "LABEL=root\t/\text4\tdiscard,errors=remount-ro\t0 1\n"
	with, _, _ := addUsrquota(fstab, "/")
	got, changed := removeUsrquota(with, "/")
	if !changed || got != fstab {
		t.Errorf("removeUsrquota = %q, %v", got, changed)
	}
	if _, changed := removeUsrquota(fstab, "/"); changed {
		t.Error("changed an fstab without usrquota")
	}
	if got, _ := removeUsrquota("/dev/sda1 / ext4 usrquota 0 1\n", "/"); got != "/dev/sda1 / ext4 defaults 0 1\n" {
		t.Errorf("only option removed: %q", got)
	}
}
