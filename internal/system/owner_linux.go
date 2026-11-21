//go:build linux

package system

import (
	"io/fs"
	"syscall"
)

// Owner returns the uid/gid that own a file.
func Owner(fi fs.FileInfo) (Credential, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Credential{}, false
	}
	return Credential{UID: st.Uid, GID: st.Gid}, true
}
