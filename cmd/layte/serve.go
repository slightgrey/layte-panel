package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/api"
	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/web"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cf := addConfigFlags(fs)
	fs.Parse(args)

	cfg, err := cf.load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.Dev)

	// A context carries cancellation through the program. NotifyContext gives us
	// one that is cancelled when SIGINT/SIGTERM arrives, which is how systemd
	// stops the service. Everything that does I/O receives this ctx (or a child of it).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	kr, err := auth.LoadOrCreateKey(cfg.SecretKeyFile)
	if err != nil {
		return err
	}
	if n, err := st.CountUsers(ctx); err == nil && n == 0 {
		log.Warn("no admin users yet; create one with: layte admin create --username NAME")
	}
	go api.CleanupSessions(ctx, st, cfg, log, 10*time.Minute)

	if n, err := st.FailInterruptedJobs(ctx); err != nil {
		return err
	} else if n > 0 {
		log.Warn("marked jobs interrupted by the last restart as failed", "count", n)
	}
	runner := jobs.New(ctx, st, log, 2)
	defer runner.Wait() // after shutdown, let jobs record their final state

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: api.New(api.Deps{
			Config:  cfg,
			Store:   st,
			Keyring: kr,
			Agent:   &agent.Client{SocketPath: cfg.AgentSocket, Auditor: storeAuditor{st}},
			Jobs:    runner,
			Log:     log,
			Version: version,
			UI:      web.Dist(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// ListenAndServe blocks, so it runs in a goroutine (a lightweight thread).
	// Its result is sent on a channel so the main goroutine can wait for either
	// a server error or a shutdown signal, whichever happens first.
	errc := make(chan error, 1)
	go func() {
		log.Info("layte serve listening", "addr", cfg.Listen, "dev", cfg.Dev, "version", version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
