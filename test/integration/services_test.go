//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
)

// TestServiceControlOnRealServer reloads and restarts real services through
// the agent, checks a broken nginx config stops the reload, and schedules a
// reboot and cancels it (the VM doesn't actually reboot).
func TestServiceControlOnRealServer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	ag := e.svc.Agent
	versions := phpVersions(t)

	var list []actions.ServiceDetail
	if err := ag.Call(ctx, "services.detail", e.actor, nil, &list); err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{}
	for _, s := range list {
		if s.Action != nil {
			ops[s.Name] = s.Action.Op
		}
	}
	if ops["nginx"] != "reload" || ops["php"+versions[0]+"-fpm"] != "reload" || ops["layte-agent"] != "" {
		t.Fatalf("actions: %v", ops)
	}

	pidOf := func(unit string) string {
		out, _ := exec.Command("systemctl", "show", "--value", "--property", "MainPID", unit).Output()
		return strings.TrimSpace(string(out))
	}
	for _, c := range []actions.ServiceControlParams{
		{Name: "nginx", Op: "reload"},
		{Name: "php" + versions[0] + "-fpm", Op: "reload"},
		{Name: "ssh", Op: "reload"},
		{Name: "cron", Op: "restart"},
	} {
		before := pidOf(c.Name + ".service")
		var out map[string]string
		if err := ag.Call(ctx, "service.control", e.actor, c, &out); err != nil {
			t.Errorf("%s %s: %v", c.Op, c.Name, err)
			continue
		}
		after := pidOf(c.Name + ".service")
		if out["active"] != "active" {
			t.Errorf("%s after %s: %v", c.Name, c.Op, out)
		}
		// A reload keeps the main process; a restart replaces it.
		if (c.Op == "reload") != (before == after) {
			t.Errorf("%s %s: main PID %s → %s", c.Op, c.Name, before, after)
		}
	}

	// A broken nginx config: the reload is refused and nginx keeps serving.
	bad := "/etc/nginx/conf.d/it-broken.conf"
	os.WriteFile(bad, []byte("server { this is not nginx }\n"), 0o644)
	err := ag.Call(ctx, "service.control", e.actor, actions.ServiceControlParams{Name: "nginx", Op: "reload"}, nil)
	os.Remove(bad)
	if err == nil || !strings.Contains(err.Error(), "config test failed") {
		t.Errorf("reload with a broken config: %v", err)
	}
	nginxTestOK(t)

	// Reboot: scheduled a minute out, then cancelled.
	var st actions.RebootStatus
	if err := ag.Call(ctx, "system.reboot", e.actor, nil, &st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("shutdown", "-c").Run() }) // never leave it scheduled
	if st.Scheduled == nil {
		t.Fatalf("reboot not scheduled: %+v", st)
	}
	var after actions.RebootStatus // fresh: "scheduled" is left out when there's none
	if err := ag.Call(ctx, "system.reboot_cancel", e.actor, nil, &after); err != nil {
		t.Fatal(err)
	}
	if after.Scheduled != nil {
		t.Errorf("reboot still scheduled after cancel: %+v", after)
	}
	if out, _ := exec.Command("shutdown", "--show").CombinedOutput(); !strings.Contains(string(out), "No scheduled shutdown") {
		t.Errorf("logind still has a shutdown: %s", out)
	}
}
