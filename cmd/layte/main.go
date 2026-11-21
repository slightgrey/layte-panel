// Command layte is the Layte Panel binary. One executable, several modes:
//
//	layte serve    HTTP API + embedded web UI (unprivileged)
//	layte agent    root helper that runs allowlisted actions over a Unix socket
//	layte admin    manage admin users (create, reset-password, reset-2fa)
//	layte version  print the version
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// version is overwritten at build time:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/layte
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = runServe(args)
	case "agent":
		err = runAgent(args)
	case "admin":
		err = runAdmin(args)
	case "fsop": // internal: file manager helper, started by the agent
		err = runFsop()
	case "version", "--version", "-v":
		fmt.Println("layte", version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Layte Panel — a lightweight control panel for unmanaged VPS.

Usage:
  layte serve   [--config PATH] [--dev]
  layte agent   [--config PATH] [--dev]
  layte admin   create|reset-password|reset-2fa --username NAME [--config PATH] [--dev]
  layte version
`)
}

// newLogger returns a JSON logger in production and a readable text logger in dev.
func newLogger(dev bool) *slog.Logger {
	if dev {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}
