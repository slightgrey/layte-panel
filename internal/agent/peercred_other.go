//go:build !linux

package agent

import "net"

// checkPeer is a no-op off Linux; the agent only runs for real on Linux.
func checkPeer(conn net.Conn, gid int) error { return nil }
