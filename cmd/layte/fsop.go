package main

import (
	"errors"
	"os"
	"runtime"

	"github.com/slightgrey/layte-panel/internal/fileops"
)

// runFsop is the file manager helper. The agent starts it as the site's
// Linux user with one JSON request on stdin; it answers on stdout and exits.
// It refuses to run as root: file operations must never be privileged.
func runFsop() error {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return errors.New("fsop must not run as root")
	}
	return fileops.Serve(os.Stdin, os.Stdout)
}
