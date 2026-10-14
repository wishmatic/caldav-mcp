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

	// firstFetchTimeout bounds the fetch a cold start makes before it serves,
	// so an unreachable server delays the port opening but never wedges it.
	firstFetchTimeout = 2 * time.Minute

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

	startFetching(ctx, cfg, syncer, cache, log)

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

// startFetching makes the first fetch and starts the ones that follow it.
//
// An empty cache is filled before anything is served, because until events land
// there is nothing to answer with, and a client asking in the meantime would be
// told the calendar is empty. A cache that already holds events is refreshed in
// the background instead, so a restart is never held up by a slow CalDAV
// server.
func startFetching(ctx context.Context, cfg config.Config, syncer *caldav.Syncer, cache *store.Store, log *zap.Logger) {
	empty, err := cache.Empty(ctx)
	if err != nil {
		// Filling it anyway costs one pass, and is the safe reading of a cache
		// that cannot be read.
		log.Warn("could not tell whether the cache is empty; fetching before serving", zap.Error(err))

		empty = true
	}

	interval := cfg.SyncInterval()

	switch {
	case empty:
		log.Info("the cache is empty, fetching before serving")

		firstCtx, cancel := context.WithTimeout(ctx, firstFetchTimeout)
		defer cancel()

		if err := syncer.Run(firstCtx); err != nil {
			log.Error("the first fetch failed; the cache stays empty until the next one", zap.Error(err))
		}
	case interval > 0:
		// A restart should not wait for the interval to refresh what it holds.
		go func() {
			if err := syncer.Run(ctx); err != nil {
				log.Error("sync failed", zap.Error(err))
			}
		}()
	}

	if interval == 0 {
		log.Info("periodic fetch disabled; run `server sync` to fill the cache")

		return
	}

	log.Info("fetching events", zap.Duration("interval", interval))

	go func() {
		if err := syncer.Loop(ctx, interval); err != nil {
			log.Error("fetch stopped", zap.Error(err))
		}
	}()
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
