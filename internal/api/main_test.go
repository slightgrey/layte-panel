package api

import "github.com/slightgrey/layte-panel/internal/sites"

// Tests don't run real nginx workers: no need to wait for them.
func init() { sites.FPMMoveGrace = 0 }
