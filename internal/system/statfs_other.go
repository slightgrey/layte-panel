//go:build !linux

package system

import "errors"

// hostStatfs is only supported on Linux; dev on other OSes uses statfs.json.
func hostStatfs(path string) (DiskUsage, error) {
	return DiskUsage{}, errors.New("statfs: only supported on linux")
}
