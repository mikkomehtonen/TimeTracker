package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"example.com/timetracker/internal/auth"
	"example.com/timetracker/internal/store"
	"example.com/timetracker/internal/web"
)

// productionIterations follows the story's password rule; tests inject a
// lower cost.
const productionIterations = 600000

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataDir := flag.String("data-dir", "./data", "directory holding timetracker.db")
	flag.Parse()

	if err := run(*addr, *dataDir); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run(addr, dataDir string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	app := web.New(db, auth.Hasher{Iterations: productionIterations})
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("TimeTracker listening on %s (data dir: %s)", addr, dataDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Print("shutting down")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			log.Printf("graceful window exceeded: %v", err)
			return srv.Close()
		}
		return nil
	}
}
