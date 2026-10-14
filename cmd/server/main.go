package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/wishmatic/caldav-mcp/internal/caldav"
	"github.com/wishmatic/caldav-mcp/internal/config"
	"github.com/wishmatic/caldav-mcp/internal/server"
	"github.com/wishmatic/caldav-mcp/internal/store"
)

const (
	shutdownTimeout = 15 * time.Second

	// syncTimeout bounds the one-shot `sync` a timer outside the container
	// might run, so a scheduled job cannot pile up.
	syncTimeout = 10 * time.Minute
)

func main() {
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	var err error

	switch mode {
	case "serve":
		err = serve()
	case "sync":
		err = syncOnce()
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [serve|sync]\n", os.Args[0])
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}

	defer log.Sync()

	servers, err := parseServers(cfg)
	if err != nil {
		return err
	}

	cache, err := store.Open(context.Background(), cfg.CachePath)
	if err != nil {
		return err
	}

	defer cache.Close()

	syncer := caldav.NewSyncer(servers, caldav.NewFetcher(cfg.SyncLookahead(), log), cache, log)

	srv, err := server.New(cfg, log, cache)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runSyncLoop(ctx, cfg, syncer, log)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		log.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			log.Error("graceful shutdown failed", zap.Error(err))
		}
	}

	log.Info("server stopped")

	return nil
}

// syncOnce fetches everything and exits, for deployments that fetch on a timer
// of their own instead of letting the server do it.
func syncOnce() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}

	defer log.Sync()

	servers, err := parseServers(cfg)
	if err != nil {
		return err
	}

	cache, err := store.Open(context.Background(), cfg.CachePath)
	if err != nil {
		return err
	}

	defer cache.Close()

	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()

	return caldav.NewSyncer(servers, caldav.NewFetcher(cfg.SyncLookahead(), log), cache, log).Run(ctx)
}

func runSyncLoop(ctx context.Context, cfg config.Config, syncer *caldav.Syncer, log *zap.Logger) {
	interval := cfg.SyncInterval()
	if interval == 0 {
		log.Info("periodic fetch disabled; run `server sync` to fill the cache")

		return
	}

	log.Info("fetching events", zap.Duration("interval", interval))

	if err := syncer.Loop(ctx, interval); err != nil {
		log.Error("fetch stopped", zap.Error(err))
	}
}

func parseServers(cfg config.Config) ([]caldav.Server, error) {
	servers, err := caldav.ParseServers(cfg.CalDAVServers)
	if err != nil {
		return nil, err
	}

	if len(servers) == 0 {
		return nil, errors.New("CALDAV_SERVERS is empty; expected name=url,name=url")
	}

	return servers, nil
}

func setup() (config.Config, *zap.Logger, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}

	if err := cfg.Validate(); err != nil {
		return config.Config{}, nil, err
	}

	log := newLogger(cfg.LogLevel)

	return cfg, log, nil
}

func newLogger(level string) *zap.Logger {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)

	log, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return log
}
