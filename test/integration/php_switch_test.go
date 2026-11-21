//go:build integration

package integration

import (
	"testing"

	"github.com/slightgrey/layte-panel/internal/store"
)

// testSwitchPHP is filled in with the PHP-switch milestone.
func testSwitchPHP(t *testing.T, e *env, site store.Site, to string) {
	t.Skip("PHP switching not implemented yet")
}
