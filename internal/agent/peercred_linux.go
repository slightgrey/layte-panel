//go:build linux

package agent

import (
	"fmt"
	"net"
	"os/user"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"
)

// checkPeer asks the kernel who is on the other end of the socket
// (SO_PEERCRED) and allows only root or members of the agent group.
// The file mode already restricts access; this is a second layer.
func checkPeer(conn net.Conn, gid int) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return fmt.Errorf("SO_PEERCRED: %w", credErr)
	}
	if cred.Uid == 0 || int(cred.Gid) == gid {
		return nil
	}
	u, err := user.LookupId(strconv.Itoa(int(cred.Uid)))
	if err != nil {
		return fmt.Errorf("peer uid %d: %w", cred.Uid, err)
	}
	groups, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("peer uid %d groups: %w", cred.Uid, err)
	}
	if slices.Contains(groups, strconv.Itoa(gid)) {
		return nil
	}
	return fmt.Errorf("peer uid %d (pid %d) is not in the agent group", cred.Uid, cred.Pid)
}
