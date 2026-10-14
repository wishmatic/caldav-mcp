package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenRejectsAnEmptyPath(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("Open() error = nil, want the missing path reported")
	}
}

func TestOpenNamesTheCachePathItCannotUse(t *testing.T) {
	// A file where a directory should be is one way to make the path unusable
	// on any platform.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	path := filepath.Join(blocker, "caldav.db")

	_, err := Open(context.Background(), path)
	if err == nil {
		t.Fatal("Open() error = nil, want the unusable path reported")
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("Open() error = %v, want it to name %s", err, path)
	}
}

func TestOpenReusesAnExistingCache(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "caldav.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	event := testEvent("standup", at(9, 0), at(9, 30))
	if err := first.Replace(ctx, "work", "Team", []Event{event}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen error: %v", err)
	}

	t.Cleanup(func() { _ = second.Close() })

	events, err := second.Between(ctx, at(0, 0), at(23, 59))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("events = %d, want the event to survive the restart", len(events))
	}
}

func TestBetweenReturnsOverlappingEventsOldestFirst(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	later := testEvent("later", at(14, 0), at(15, 0))
	earlier := testEvent("earlier", at(9, 0), at(10, 0))

	if err := cache.Replace(ctx, "work", "Team", []Event{later, earlier}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	events, err := cache.Between(ctx, at(8, 0), at(16, 0))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}

	if events[0].UID != "earlier" || events[1].UID != "later" {
		t.Errorf("order = %s, %s, want oldest first", events[0].UID, events[1].UID)
	}

	if events[0].Start != at(9, 0) || events[0].End != at(10, 0) {
		t.Errorf("start/end = %v/%v, want the stored times back", events[0].Start, events[0].End)
	}

	if events[0].Summary != "earlier" || events[0].AllDay {
		t.Errorf("event = %+v, want the stored fields back", events[0])
	}
}

func TestBetweenKeepsEventsReachingIntoTheWindow(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	ongoing := testEvent("ongoing", at(8, 0), at(11, 0))
	past := testEvent("past", at(6, 0), at(7, 0))

	if err := cache.Replace(ctx, "work", "Team", []Event{ongoing, past}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	events, err := cache.Between(ctx, at(10, 0), at(12, 0))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 1 || events[0].UID != "ongoing" {
		t.Errorf("events = %+v, want only the event still running", events)
	}
}

func TestReplaceDropsWhatTheCalendarHeldBefore(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	if err := cache.Replace(ctx, "work", "Team", []Event{testEvent("old", at(9, 0), at(10, 0))}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	if err := cache.Replace(ctx, "work", "Team", []Event{testEvent("new", at(9, 0), at(10, 0))}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	events, err := cache.Between(ctx, at(0, 0), at(23, 59))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 1 || events[0].UID != "new" {
		t.Errorf("events = %+v, want only what the last fetch returned", events)
	}
}

func TestReplaceLeavesOtherCalendarsAlone(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	if err := cache.Replace(ctx, "work", "Team", []Event{testEvent("standup", at(9, 0), at(10, 0))}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	if err := cache.Replace(ctx, "work", "Personal", []Event{testEvent("dentist", at(11, 0), at(12, 0))}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	events, err := cache.Between(ctx, at(0, 0), at(23, 59))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want both calendars", len(events))
	}
}

func TestReplaceKeepsOccurrencesOfARecurringEventApart(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	first := testEvent("standup", at(9, 0), at(9, 30))
	first.Recurrence = at(9, 0).Format(time.RFC3339)

	second := testEvent("standup", at(9, 0).AddDate(0, 0, 1), at(9, 30).AddDate(0, 0, 1))
	second.Recurrence = at(9, 0).AddDate(0, 0, 1).Format(time.RFC3339)

	if err := cache.Replace(ctx, "work", "Team", []Event{first, second}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	events, err := cache.Between(ctx, at(0, 0), at(0, 0).AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want both occurrences", len(events))
	}

	if events[0].Recurrence == events[1].Recurrence {
		t.Error("the occurrences share a recurrence key, so one would overwrite the other")
	}
}

func TestPruneDropsFinishedEvents(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	finished := testEvent("finished", at(9, 0), at(10, 0))
	upcoming := testEvent("upcoming", at(13, 0), at(14, 0))

	if err := cache.Replace(ctx, "work", "Team", []Event{finished, upcoming}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	removed, err := cache.Prune(ctx, at(11, 0))
	if err != nil {
		t.Fatalf("Prune() error: %v", err)
	}

	if removed != 1 {
		t.Errorf("Prune() = %d, want 1", removed)
	}

	events, err := cache.Between(ctx, at(0, 0), at(23, 59))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 1 || events[0].UID != "upcoming" {
		t.Errorf("events = %+v, want only the upcoming event", events)
	}
}

func TestPruneKeepsRunningEvents(t *testing.T) {
	ctx := context.Background()
	cache := open(t)

	running := testEvent("running", at(9, 0), at(12, 0))
	if err := cache.Replace(ctx, "work", "Team", []Event{running}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	if _, err := cache.Prune(ctx, at(10, 0)); err != nil {
		t.Fatalf("Prune() error: %v", err)
	}

	events, err := cache.Between(ctx, at(10, 0), at(23, 59))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("events = %d, want the event that is still running", len(events))
	}
}

func open(t *testing.T) *Store {
	t.Helper()

	cache, err := Open(context.Background(), filepath.Join(t.TempDir(), "caldav.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	t.Cleanup(func() { _ = cache.Close() })

	return cache
}

func testEvent(uid string, start, end time.Time) Event {
	return Event{
		Server:      "work",
		Calendar:    "Team",
		UID:         uid,
		Start:       start,
		End:         end,
		Summary:     uid,
		Description: "description of " + uid,
		Location:    "somewhere",
	}
}

func at(hour, minute int) time.Time {
	return time.Date(2026, 3, 2, hour, minute, 0, 0, time.UTC)
}
