//go:build linux

package system

import (
	"fmt"
	"syscall"
)

// hostStatfs reports disk capacity for the filesystem holding path.
// The `//go:build linux` line at the top means this file is only compiled on Linux.
func hostStatfs(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	bs := uint64(st.Bsize)
	return DiskUsage{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}
