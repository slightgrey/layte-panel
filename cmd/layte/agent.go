package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"

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

	deps := actions.HostDeps()
	if cfg.Dev {
		// Dev mode never touches the real machine: commands and /proc reads
		// are answered from embedded fixtures.
		deps = actions.DevDeps()
		log.Warn("agent running in dev mode with FAKE system data")
	} else {
		if runtime.GOOS != "linux" {
			return errors.New("the agent only runs on Linux; use --dev for local development")
		}
		if os.Geteuid() != 0 {
			return errors.New("the agent must run as root (it is started by systemd as layte-agent.service)")
		}
	}

	reg := agent.NewRegistry()
	actions.Register(reg, deps)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &agent.Server{
		Registry:   reg,
		Log:        log,
		SocketPath: cfg.AgentSocket,
		Group:      "layte",
		Dev:        cfg.Dev,
	}
	return srv.Serve(ctx)
}
