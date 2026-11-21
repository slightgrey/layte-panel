//go:build !linux

package system

import "errors"

// Statfs is only supported on Linux; dev on other OSes uses FixtureFS.
func (HostFS) Statfs(path string) (DiskUsage, error) {
	return DiskUsage{}, errors.New("statfs: only supported on linux")
}
