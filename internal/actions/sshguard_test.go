package actions

import (
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
)

func TestSSHGuardStatus(t *testing.T) {
	d := devDeps(t)
	out, err := d.securityOverview(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	st := out.(SecurityOverview).SSHGuard
	if !st.Installed || !st.Running || st.BlockedNow != 4 || st.Permanent != 3 || st.Attacks24h != 4 || st.Blocks24h != 2 {
		t.Errorf("status: %+v", st)
	}
	if len(st.Recent) != 2 || st.Recent[0].Address != "192.0.2.236" || st.Recent[0].For != "180 secs" || st.Recent[0].Time.Hour() != 16 {
		t.Errorf("recent (newest first, /32 dropped): %+v", st.Recent)
	}

	// Not installed: everything zero, never an error.
	d.FS.Remove(sshguardBin)
	out, _ = d.securityOverview(ctx, agent.Actor{}, agent.NoParams{})
	if st := out.(SecurityOverview).SSHGuard; st.Installed || st.Running || st.Recent == nil {
		t.Errorf("not installed: %+v", st)
	}
}

func TestNftElements(t *testing.T) {
	if n := nftElements("table ip sshguard {\n\tset attackers {\n\t\ttype ipv4_addr\n\t}\n}\n"); n != 0 {
		t.Errorf("empty set: %d", n)
	}
	if n := nftElements("elements = { 1.2.3.4, 5.6.7.0/24,\n\t 8.8.8.8 }"); n != 3 {
		t.Errorf("three: %d", n)
	}
}
