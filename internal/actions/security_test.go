package actions

import "testing"

// Real `ufw status numbered` output (Ubuntu 24.04), including v6 rows and app profiles.
const ufwNumbered = `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] Nginx Full                 ALLOW IN    Anywhere
[ 3] 3306                       ALLOW IN    10.0.0.5
[ 4] 8000:8100/tcp              DENY IN     Anywhere
[ 5] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
[ 6] Nginx Full (v6)            ALLOW IN    Anywhere (v6)
`

func TestParseUFWNumbered(t *testing.T) {
	rules := parseUFWNumbered(ufwNumbered, []string{"22"})
	if len(rules) != 6 {
		t.Fatalf("got %d rules: %+v", len(rules), rules)
	}
	want := []struct {
		to, action, from string
		v6, locked       bool
	}{
		{"22/tcp", "ALLOW IN", "Anywhere", false, true},
		{"Nginx Full", "ALLOW IN", "Anywhere", false, true},
		{"3306", "ALLOW IN", "10.0.0.5", false, false},
		{"8000:8100/tcp", "DENY IN", "Anywhere", false, false},
		{"22/tcp", "ALLOW IN", "Anywhere (v6)", true, true},
		{"Nginx Full", "ALLOW IN", "Anywhere (v6)", true, true},
	}
	for i, w := range want {
		r := rules[i]
		if r.Num != i+1 || r.To != w.to || r.Action != w.action || r.From != w.from || r.V6 != w.v6 || r.Locked != w.locked {
			t.Errorf("rule %d = %+v, want %+v", i+1, r, w)
		}
	}
	if rules[4].Text != "22/tcp (v6) ALLOW IN Anywhere (v6)" {
		t.Errorf("text = %q", rules[4].Text)
	}
}

func TestSSHPorts(t *testing.T) {
	d := devDeps(t)
	if p := d.sshPorts(); len(p) != 1 || p[0] != "22" {
		t.Errorf("default: %v", p)
	}
	d.FS.WriteFileAtomic("/etc/ssh/sshd_config.d/port.conf", []byte("Port 2222\n"), 0o644)
	if p := d.sshPorts(); len(p) != 1 || p[0] != "2222" {
		t.Errorf("drop-in: %v", p)
	}
}
