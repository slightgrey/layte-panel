package api

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

// debugAgent answers wp.debug_get / wp.debug_set and remembers the state.
type debugAgent struct{ on *bool }

func (a debugAgent) Call(_ context.Context, action string, _ agent.Actor, params, out any) error {
	switch action {
	case "wp.debug_get":
	case "wp.debug_set":
		*a.on = params.(actions.WPDebugSetParams).Enabled
	default:
		return agent.Errorf(agent.CodeInvalidParams, "unexpected %s", action)
	}
	b, _ := json.Marshal(actions.WPDebugState{Enabled: *a.on, LogPath: "/home/blog/blog.example.com/logs/wp-debug.log"})
	return json.Unmarshal(b, out)
}

func TestWPDebugAPI(t *testing.T) {
	on := false
	env := newTestEnvWithAgent(t, debugAgent{on: &on})
	ctx := context.Background()
	id, err := env.st.CreateSite(ctx, store.Site{Domain: "blog.example.com", SystemUser: "blog", PHPVersion: "8.3",
		Managed: true, Source: "created", WordPress: true, Status: "active", RootPath: "/home/blog/blog.example.com/public"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.st.CreateSite(ctx, store.Site{Domain: "static.example.com", SystemUser: "static", PHPVersion: "8.3",
		Managed: true, Source: "created", Status: "active", RootPath: "/home/static/static.example.com/public"})
	if err != nil {
		t.Fatal(err)
	}
	c := env.loggedIn(t)
	url := "/api/v1/sites/" + strconv.FormatInt(id, 10) + "/wp-debug"

	if code, body := c.do("GET", url, nil); code != 200 || body["enabled"] != false || body["log_path"] == "" {
		t.Fatalf("get: %d %v", code, body)
	}
	if code, body := c.do("PUT", url, map[string]any{"enabled": true}); code != 200 || body["enabled"] != true {
		t.Fatalf("put: %d %v", code, body)
	}
	if !on {
		t.Error("agent not told to enable")
	}
	if code, _ := c.do("GET", "/api/v1/sites/"+strconv.FormatInt(plain, 10)+"/wp-debug", nil); code != 400 {
		t.Errorf("non-WordPress site: %d", code)
	}
	if code, _ := env.client(t).do("GET", url, nil); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
}
