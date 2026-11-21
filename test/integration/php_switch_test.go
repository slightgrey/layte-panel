//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
)

// testSwitchPHP moves the site to another installed PHP version and checks
// it keeps serving, with the old pool gone.
func testSwitchPHP(t *testing.T, e *env, site store.Site, to string) {
	from := site.PHPVersion
	jobID, err := e.svc.SwitchPHP(context.Background(), e.actor, site, to)
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	nginxTestOK(t)

	if _, err := os.Stat(templates.PoolPath(to, site.Domain)); err != nil {
		t.Errorf("new pool missing: %v", err)
	}
	if _, err := os.Stat(templates.PoolPath(from, site.Domain)); !os.IsNotExist(err) {
		t.Errorf("old pool still present")
	}
	if body := get(t, site.Domain); !strings.HasPrefix(body, "200") && !strings.HasPrefix(body, "30") {
		t.Errorf("site not serving after switch:\n%.300s", body)
	}
}
