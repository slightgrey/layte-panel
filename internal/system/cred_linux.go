//go:build linux

package system

import (
	"os/exec"
	"syscall"
)

// setCredential makes the child process run as another user. Groups is set
// to an empty list so the child doesn't inherit root's supplementary groups.
func setCredential(cmd *exec.Cmd, c Credential) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: c.UID, Gid: c.GID, Groups: []uint32{}},
	}
	return nil
}
