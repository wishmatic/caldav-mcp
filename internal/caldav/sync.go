package caldav

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

// Syncer copies what the servers hold into the cache.
type Syncer struct {
	servers []Server
	fetcher *Fetcher
	store   *store.Store
	log     *zap.Logger
}

func NewSyncer(servers []Server, fetcher *Fetcher, st *store.Store, log *zap.Logger) *Syncer {
	return &Syncer{servers: servers, fetcher: fetcher, store: st, log: log}
}

// Run fetches every server once. One unreachable server does not stop the
// others, and every failure is reported together.
func (s *Syncer) Run(ctx context.Context) error {
	var errs []error

	for _, srv := range s.servers {
		if err := s.one(ctx, srv); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", srv.Name, err))
		}
	}

	pruned, err := s.store.Prune(ctx, time.Now())
	if err != nil {
		errs = append(errs, err)
	} else if pruned > 0 {
		s.log.Info("pruned finished events", zap.Int64("events", pruned))
	}

	return errors.Join(errs...)
}

// Loop runs a pass immediately, then again every interval until ctx is done.
func (s *Syncer) Loop(ctx context.Context, interval time.Duration) error {
	if err := s.Run(ctx); err != nil {
		s.log.Error("sync failed", zap.Error(err))
	}

	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.Run(ctx); err != nil {
				s.log.Error("sync failed", zap.Error(err))
			}
		}
	}
}

func (s *Syncer) one(ctx context.Context, srv Server) error {
	calendars, err := s.fetcher.Calendars(ctx, srv)
	if err != nil {
		return err
	}

	if len(calendars) == 0 {
		s.log.Warn("no calendars to fetch", zap.String("server", srv.Name))

		return nil
	}

	var errs []error

	for _, calendar := range calendars {
		events, err := s.fetcher.Events(ctx, srv, calendar)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if err := s.store.Replace(ctx, srv.Name, calendar.Name, events); err != nil {
			errs = append(errs, err)

			continue
		}

		s.log.Info("cached calendar",
			zap.String("server", srv.Name),
			zap.String("calendar", calendar.Name),
			zap.Int("events", len(events)),
		)
	}

	return errors.Join(errs...)
}
