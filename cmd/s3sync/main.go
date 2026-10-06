package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"s3sync/internal/api"
	"s3sync/internal/config"
	"s3sync/internal/engine"
	"s3sync/internal/scheduler"
	"s3sync/internal/secret"
	"s3sync/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	box, err := secret.New(cfg.MasterKey)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "s3sync.db"), box)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.FailInterruptedRuns(); err != nil {
		return err
	}

	tmpDir := filepath.Join(cfg.DataDir, "tmp")
	os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return err
	}

	eng := engine.New(st, tmpDir)
	sched := scheduler.New(st, eng)
	if err := sched.Reload(); err != nil {
		return err
	}
	sched.Start()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(cfg, st, eng, sched).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "data_dir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case s := <-sig:
		slog.Info("shutting down", "signal", s.String())
	}

	sched.Stop()
	eng.Shutdown(20 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// healthcheck is used by the container HEALTHCHECK (the image has no curl).
func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/healthz", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
