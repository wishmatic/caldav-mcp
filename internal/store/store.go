// Package store caches calendar events in SQLite so the MCP tool can answer
// without waiting on a CalDAV server.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
	server      TEXT    NOT NULL,
	calendar    TEXT    NOT NULL,
	uid         TEXT    NOT NULL,
	recurrence  TEXT    NOT NULL,
	starts_at   INTEGER NOT NULL,
	ends_at     INTEGER NOT NULL,
	all_day     INTEGER NOT NULL,
	summary     TEXT    NOT NULL,
	description TEXT    NOT NULL,
	location    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS events_scope ON events (server, calendar);
CREATE INDEX IF NOT EXISTS events_starts_at ON events (starts_at);
`

// Event is one occurrence of one calendar event. Recurring events arrive here
// already expanded, so every row stands on its own.
type Event struct {
	Server      string
	Calendar    string
	UID         string
	Recurrence  string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Summary     string
	Description string
	Location    string
}

type Store struct {
	db *sql.DB
}

// Open prepares the cache at path, creating the file and schema if needed.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("cache path is empty")
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// One connection keeps this process from racing itself, and is far more
	// than a cache of this size needs. Cross-process contention is left to the
	// busy timeout, which the DSN sets.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &Store{db: db}, nil
}

// The pragmas belong in the DSN rather than an Exec so that the driver applies
// them to every connection it opens, not just the first.
func dsn(path string) string {
	return path + "?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Replace makes events the entire cached content of one calendar, dropping
// whatever that calendar held before. The swap is one transaction, so readers
// never see half of it.
func (s *Store) Replace(ctx context.Context, server, calendar string, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM events WHERE server = ? AND calendar = ?`, server, calendar,
	); err != nil {
		return fmt.Errorf("clear %s/%s: %w", server, calendar, err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events
			(server, calendar, uid, recurrence, starts_at, ends_at, all_day, summary, description, location)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}

	defer func() { _ = stmt.Close() }()

	for _, e := range events {
		_, err := stmt.ExecContext(ctx,
			e.Server, e.Calendar, e.UID, e.Recurrence,
			e.Start.Unix(), e.End.Unix(), e.AllDay,
			e.Summary, e.Description, e.Location,
		)
		if err != nil {
			return fmt.Errorf("insert %s: %w", e.UID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// Prune drops occurrences that already ended, which bounds the cache by the
// fetch window rather than by how long the process has been running.
func (s *Store) Prune(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ends_at < ?`, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune: %w", err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned: %w", err)
	}

	return removed, nil
}

// Between returns the cached occurrences overlapping [from, to], oldest first.
func (s *Store) Between(ctx context.Context, from, to time.Time) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server, calendar, uid, recurrence, starts_at, ends_at, all_day, summary, description, location
		FROM events
		WHERE ends_at >= ? AND starts_at <= ?
		ORDER BY starts_at, summary`, from.Unix(), to.Unix())
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		var (
			event      Event
			start, end int64
		)

		if err := rows.Scan(
			&event.Server, &event.Calendar, &event.UID, &event.Recurrence,
			&start, &end, &event.AllDay,
			&event.Summary, &event.Description, &event.Location,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		event.Start = time.Unix(start, 0).UTC()
		event.End = time.Unix(end, 0).UTC()

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	return events, nil
}
