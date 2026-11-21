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

	"layte-panel/internal/api"
	"layte-panel/internal/store"
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

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(api.Deps{Config: cfg, Store: st, Log: log, Version: version}),
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
