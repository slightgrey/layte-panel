package system

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os/user"
	"strconv"
)

// ErrNoUser means the Linux user doesn't exist.
var ErrNoUser = errors.New("no such user")

// Users looks up Linux accounts.
type Users interface {
	Lookup(name string) (Credential, error)
}

// HostUsers reads /etc/passwd via os/user (pure Go, no CGO needed).
type HostUsers struct{}

func (HostUsers) Lookup(name string) (Credential, error) {
	u, err := user.Lookup(name)
	if err != nil {
		var unk user.UnknownUserError
		if errors.As(err, &unk) {
			return Credential{}, fmt.Errorf("user %s: %w", name, ErrNoUser)
		}
		return Credential{}, err
	}
	uid, err1 := strconv.ParseUint(u.Uid, 10, 32)
	gid, err2 := strconv.ParseUint(u.Gid, 10, 32)
	if err1 != nil || err2 != nil {
		return Credential{}, fmt.Errorf("user %s: bad uid/gid", name)
	}
	return Credential{UID: uint32(uid), GID: uint32(gid)}, nil
}

// DirUsers is the dev fake: a user "exists" when /home/<name> exists in the FS.
type DirUsers struct{ FS FS }

func (d DirUsers) Lookup(name string) (Credential, error) {
	if name == "www-data" {
		return Credential{UID: 33, GID: 33}, nil
	}
	ok, err := d.FS.Exists("/home/" + name)
	if err != nil {
		return Credential{}, err
	}
	if !ok {
		return Credential{}, fmt.Errorf("user %s: %w", name, ErrNoUser)
	}
	// A stable, distinct id per name (like real users have).
	h := fnv.New32a()
	h.Write([]byte(name))
	id := 1001 + h.Sum32()%50000
	return Credential{UID: id, GID: id}, nil
}
