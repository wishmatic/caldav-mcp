package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

func TestGetEventsDefaultsToFourteenDays(t *testing.T) {
	source := &fakeEvents{}

	_, output, err := newHandlers(source).getEvents(context.Background(), nil, getEventsInput{})
	if err != nil {
		t.Fatalf("getEvents() error: %v", err)
	}

	assertWindow(t, output, 14)
}

func TestGetEventsHonoursTheRequestedDuration(t *testing.T) {
	source := &fakeEvents{}

	for _, days := range []int{1, 7, 30} {
		_, output, err := newHandlers(source).getEvents(context.Background(), nil, getEventsInput{Duration: days})
		if err != nil {
			t.Fatalf("getEvents() error: %v", err)
		}

		assertWindow(t, output, days)
	}
}

func TestGetEventsMapsTheCachedEvents(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	source := &fakeEvents{events: []store.Event{{
		Server:      "work",
		Calendar:    "Team",
		UID:         "sync@example.com",
		Recurrence:  start.Format(time.RFC3339),
		Start:       start,
		End:         start.Add(30 * time.Minute),
		AllDay:      true,
		Summary:     "Weekly sync",
		Description: "Notes",
		Location:    "Room 2",
	}}}

	_, output, err := newHandlers(source).getEvents(context.Background(), nil, getEventsInput{Duration: 10})
	if err != nil {
		t.Fatalf("getEvents() error: %v", err)
	}

	if len(output.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(output.Events))
	}

	got := output.Events[0]

	if got.ID != "sync@example.com" || got.Summary != "Weekly sync" {
		t.Errorf("event = %+v, want the cached summary and UID", got)
	}

	if got.Start != "2026-03-01T09:00:00Z" || got.End != "2026-03-01T09:30:00Z" {
		t.Errorf("start/end = %s/%s, want RFC 3339 UTC", got.Start, got.End)
	}

	if !got.AllDay || got.Location != "Room 2" || got.Description != "Notes" {
		t.Errorf("event = %+v, want the remaining cached fields", got)
	}

	if got.Calendar != "Team" || got.Server != "work" {
		t.Errorf("event = %+v, want the calendar and server it came from", got)
	}
}

func TestGetEventsReturnsAnEmptyList(t *testing.T) {
	_, output, err := newHandlers(&fakeEvents{}).getEvents(context.Background(), nil, getEventsInput{})
	if err != nil {
		t.Fatalf("getEvents() error: %v", err)
	}

	if output.Events == nil {
		t.Error("Events = nil, want an empty list so the caller can tell it apart from a failure")
	}
}

func TestGetEventsReportsACacheFailure(t *testing.T) {
	failure := errors.New("cache is gone")

	_, _, err := newHandlers(&fakeEvents{err: failure}).getEvents(context.Background(), nil, getEventsInput{})
	if !errors.Is(err, failure) {
		t.Errorf("getEvents() error = %v, want %v", err, failure)
	}
}

func newHandlers(source EventSource) *handlers {
	return &handlers{log: zap.NewNop(), events: source}
}

func assertWindow(t *testing.T, output getEventsOutput, days int) {
	t.Helper()

	from, err := time.Parse(time.RFC3339, output.From)
	if err != nil {
		t.Fatalf("From = %q: %v", output.From, err)
	}

	to, err := time.Parse(time.RFC3339, output.To)
	if err != nil {
		t.Fatalf("To = %q: %v", output.To, err)
	}

	if hours := to.Sub(from).Hours() / 24; hours < float64(days)-0.01 || hours > float64(days)+0.01 {
		t.Errorf("window = %.2f days, want %d", hours, days)
	}
}
