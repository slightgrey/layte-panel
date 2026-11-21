package actions

import (
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

// devDeps returns fake dependencies rooted at a fresh copy of the fixtures.
func devDeps(t *testing.T) Deps {
	t.Helper()
	d, err := DevDeps(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Close the root handle before TempDir cleanup (Windows can't delete open dirs).
	t.Cleanup(func() { d.FS.(*system.RootFS).Close() })
	return d
}
