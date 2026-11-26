//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

// TestReadOnlyActions calls the dashboard, updates and security actions
// through the real agent, so a new OS release that changes a tool's output
// shows up here. The JSON goes to the test log for a quick look.
func TestReadOnlyActions(t *testing.T) {
	e := setup(t)
	ag := e.svc.Agent
	for _, tt := range []struct {
		action string
		params any
	}{
		{"system.info", agent.NoParams{}},
		{"services.status", agent.NoParams{}},
		{"versions.get", agent.NoParams{}},
		{"php.versions", agent.NoParams{}},
		{"updates.pending", agent.NoParams{}},
		{"updates.plan", actions.UpgradeParams{}},
		{"security.overview", agent.NoParams{}},
		{"sites.discover", agent.NoParams{}},
		{"ssl.list", agent.NoParams{}},
		{"db.list", agent.NoParams{}},
		{"logs.sources", agent.NoParams{}},
		{"system.processes", agent.NoParams{}},
	} {
		t.Run(tt.action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			var out json.RawMessage
			if err := ag.Call(ctx, tt.action, e.actor, tt.params, &out); err != nil {
				t.Fatal(err)
			}
			t.Logf("%.1500s", out)
		})
	}
}
