package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	cf := addConfigFlags(fs)
	fs.Parse(args)

	cfg, err := cf.load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.Dev)

	var deps actions.Deps
	if cfg.Dev {
		// Dev mode never touches the real machine: files live in a fake root
		// (seeded from embedded fixtures) and commands are faked.
		fakeRoot := filepath.Join(".dev", "fakeroot")
		if deps, err = actions.DevDeps(fakeRoot); err != nil {
			return err
		}
		log.Warn("agent running in dev mode with FAKE system data", "root", fakeRoot)
	} else {
		if runtime.GOOS != "linux" {
			return errors.New("the agent only runs on Linux; use --dev for local development")
		}
		if os.Geteuid() != 0 {
			return errors.New("the agent must run as root (it is started by systemd as layte-agent.service)")
		}
		if deps, err = actions.HostDeps(); err != nil {
			return err
		}
	}

	reg := agent.NewRegistry()
	actions.Register(reg, deps)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Page cache: purge a site's cache soon after WordPress content changes
	// (its must-use plugin leaves a marker file; see actions.WatchCachePurges).
	go deps.WatchCachePurges(ctx, 15*time.Second, log)

	srv := &agent.Server{
		Registry:   reg,
		Log:        log,
		SocketPath: cfg.AgentSocket,
		Group:      "layte",
		Dev:        cfg.Dev,
	}
	return srv.Serve(ctx)
}
