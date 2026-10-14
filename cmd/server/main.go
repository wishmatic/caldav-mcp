package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/wishmatic/go-mcp/internal/config"
	"github.com/wishmatic/go-mcp/internal/server"
)

const shutdownTimeout = 15 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log := newLogger(cfg.LogLevel)
	defer log.Sync()

	srv, err := server.New(cfg, log)
	if err != nil {
		log.Error("server setup failed", zap.Error(err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Error("server failed", zap.Error(err))
			os.Exit(1)
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
