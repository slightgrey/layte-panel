//go:build !linux

package system

import (
	"errors"
	"os/exec"
)

func setCredential(*exec.Cmd, Credential) error {
	return errors.New("running commands as another user is only supported on linux")
}
