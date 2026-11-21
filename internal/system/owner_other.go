//go:build !linux

package system

import "io/fs"

// Owner is unknown off Linux (dev mode).
func Owner(fi fs.FileInfo) (Credential, bool) { return Credential{}, false }
