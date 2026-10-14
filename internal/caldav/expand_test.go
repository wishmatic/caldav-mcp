package caldav

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

// The window the tests expand into unless they say otherwise.
var (
	windowStart = date(2026, 3, 2, 0, 0)
	windowEnd   = date(2026, 3, 17, 0, 0)
)

func TestExpandASingleEvent(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:dentist@example.com
DTSTART:20260303T140000Z
DTEND:20260303T150000Z
SUMMARY:Dentist
DESCRIPTION:Bring the form
LOCATION:Room 2
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	got := events[0]

	if got.UID != "dentist@example.com" || got.Summary != "Dentist" {
		t.Errorf("event = %+v, want the summary and UID", got)
	}

	if got.Description != "Bring the form" || got.Location != "Room 2" {
		t.Errorf("event = %+v, want the description and location", got)
	}

	if got.Calendar != "Team" || got.Server != "work" {
		t.Errorf("event = %+v, want it attributed to its calendar", got)
	}

	if got.AllDay {
		t.Error("allDay = true, want false")
	}

	if got.Recurrence != "" {
		t.Errorf("recurrence = %q, want empty for a one-off event", got.Recurrence)
	}

	assertStarts(t, events, "2026-03-03T14:00:00Z")
}

func TestExpandLeavesOutEventsOutsideTheWindow(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:past@example.com
DTSTART:20260101T090000Z
DTEND:20260101T100000Z
SUMMARY:Past
END:VEVENT
BEGIN:VEVENT
UID:future@example.com
DTSTART:20261201T090000Z
DTEND:20261201T100000Z
SUMMARY:Future
END:VEVENT`)

	if len(events) != 0 {
		t.Errorf("events = %+v, want neither the past one nor the far future one", events)
	}
}

func TestExpandKeepsAnEventReachingIntoTheWindow(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:conference@example.com
DTSTART:20260228T090000Z
DTEND:20260303T170000Z
SUMMARY:Conference
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want the event that is still running", len(events))
	}

	if got := events[0].End.Format(time.RFC3339); got != "2026-03-03T17:00:00Z" {
		t.Errorf("end = %q, want the real end of the event", got)
	}
}

func TestExpandAnAllDayEvent(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:holiday@example.com
DTSTART;VALUE=DATE:20260305
DTEND;VALUE=DATE:20260306
SUMMARY:Bank holiday
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if !events[0].AllDay {
		t.Error("allDay = false, want true")
	}

	assertStarts(t, events, "2026-03-05T00:00:00Z")

	if got := events[0].End.Format(time.RFC3339); got != "2026-03-06T00:00:00Z" {
		t.Errorf("end = %q, want the exclusive DTEND", got)
	}
}

func TestExpandAnAllDayEventWithoutDTEND(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:holiday@example.com
DTSTART;VALUE=DATE:20260305
SUMMARY:Bank holiday
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if got := events[0].End.Format(time.RFC3339); got != "2026-03-06T00:00:00Z" {
		t.Errorf("end = %q, want one whole day", got)
	}
}

func TestExpandUsesDurationWhenThereIsNoDTEND(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:call@example.com
DTSTART:20260303T140000Z
DURATION:PT90M
SUMMARY:Call
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if got := events[0].End.Format(time.RFC3339); got != "2026-03-03T15:30:00Z" {
		t.Errorf("end = %q, want DTSTART plus the duration", got)
	}
}

func TestExpandRecurringEvents(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z", "2026-03-09T09:00:00Z", "2026-03-16T09:00:00Z")

	for _, event := range events {
		if event.UID != "standup@example.com" {
			t.Errorf("uid = %q, want the master's UID", event.UID)
		}

		if got := event.End.Sub(event.Start); got != 30*time.Minute {
			t.Errorf("duration = %v, want the master's 30m", got)
		}

		if event.Recurrence == "" {
			t.Error("recurrence = empty, want each occurrence told apart from the others")
		}
	}

	if events[0].Recurrence == events[1].Recurrence {
		t.Error("two occurrences share a recurrence key")
	}
}

func TestExpandHonoursUntil(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO;UNTIL=20260309T090000Z
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z", "2026-03-09T09:00:00Z")
}

func TestExpandDropsAnExcludedDate(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
EXDATE:20260309T090000Z
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z", "2026-03-16T09:00:00Z")
}

func TestExpandDropsEveryExcludedDateInAList(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
EXDATE:20260309T090000Z,20260316T090000Z
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z")
}

func TestExpandAddsExtraDatesToASeries(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
RDATE:20260304T140000Z
END:VEVENT`)

	assertStarts(t, events,
		"2026-03-02T09:00:00Z",
		"2026-03-04T14:00:00Z",
		"2026-03-09T09:00:00Z",
		"2026-03-16T09:00:00Z",
	)
}

func TestExpandAddsExtraDatesWithoutARule(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RDATE:20260304T090000Z
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z", "2026-03-04T09:00:00Z")
}

func TestExpandMovesAnOverriddenOccurrence(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT
BEGIN:VEVENT
UID:standup@example.com
RECURRENCE-ID:20260309T090000Z
DTSTART:20260310T140000Z
DTEND:20260310T150000Z
SUMMARY:Standup, moved
END:VEVENT`)

	assertStarts(t, events,
		"2026-03-02T09:00:00Z",
		"2026-03-10T14:00:00Z",
		"2026-03-16T09:00:00Z",
	)

	if got := events[1].Summary; got != "Standup, moved" {
		t.Errorf("summary = %q, want the override's own", got)
	}

	if got := events[1].Recurrence; got != "2026-03-09T09:00:00Z" {
		t.Errorf("recurrence = %q, want the occurrence the override replaced", got)
	}
}

func TestExpandDropsACancelledOccurrence(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT
BEGIN:VEVENT
UID:standup@example.com
RECURRENCE-ID:20260309T090000Z
DTSTART:20260309T090000Z
DTEND:20260309T093000Z
STATUS:CANCELLED
SUMMARY:Standup
END:VEVENT`)

	assertStarts(t, events, "2026-03-02T09:00:00Z", "2026-03-16T09:00:00Z")
}

func TestExpandDropsACancelledEvent(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260303T140000Z
DTEND:20260303T150000Z
STATUS:CANCELLED
SUMMARY:Dentist
END:VEVENT`)

	if len(events) != 0 {
		t.Errorf("events = %+v, want a cancelled event left out", events)
	}
}

func TestExpandKeepsTextTogether(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART:20260303T140000Z
DTEND:20260303T150000Z
SUMMARY:Standup, moved
DESCRIPTION:Talk about one\, two and three
LOCATION:Room 2, floor 3
END:VEVENT`)

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if got := events[0].Summary; got != "Standup, moved" {
		t.Errorf("summary = %q, want the comma kept", got)
	}

	if got := events[0].Description; got != "Talk about one, two and three" {
		t.Errorf("description = %q, want the escaped comma decoded", got)
	}

	if got := events[0].Location; got != "Room 2, floor 3" {
		t.Errorf("location = %q, want the comma kept", got)
	}
}

func TestExpandReadsTimesInTheirZone(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:call@example.com
DTSTART;TZID=Europe/Berlin:20260303T090000
DTEND;TZID=Europe/Berlin:20260303T100000
SUMMARY:Call
END:VEVENT`)

	assertStarts(t, events, "2026-03-03T08:00:00Z")
}

func TestExpandKeepsTheWallClockAcrossADaylightSavingChange(t *testing.T) {
	// Berlin moves to summer time on 29 March 2026.
	events := expandAllBetween(t, `
BEGIN:VEVENT
UID:standup@example.com
DTSTART;TZID=Europe/Berlin:20260323T090000
DTEND;TZID=Europe/Berlin:20260323T093000
SUMMARY:Standup
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT`, date(2026, 3, 20, 0, 0), date(2026, 4, 10, 0, 0))

	assertStarts(t, events,
		"2026-03-23T08:00:00Z",
		"2026-03-30T07:00:00Z",
		"2026-04-06T07:00:00Z",
	)
}

func TestExpandFallsBackWhenTheZoneIsUnknown(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:call@example.com
DTSTART;TZID=W. Europe Standard Time:20260303T090000
DTEND;TZID=W. Europe Standard Time:20260303T100000
SUMMARY:Call
END:VEVENT`)

	assertStarts(t, events, "2026-03-03T09:00:00Z")
}

func TestExpandSkipsEventsItCannotPlace(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:no-start@example.com
DTEND:20260303T150000Z
SUMMARY:No start
END:VEVENT
BEGIN:VEVENT
DTSTART:20260303T140000Z
DTEND:20260303T150000Z
SUMMARY:No uid
END:VEVENT`)

	if len(events) != 0 {
		t.Errorf("events = %+v, want the unusable ones skipped", events)
	}
}

func TestExpandKeepsSeparateSeriesApart(t *testing.T) {
	events := expandAll(t, `
BEGIN:VEVENT
UID:first@example.com
DTSTART:20260302T090000Z
DTEND:20260302T093000Z
SUMMARY:First
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT
BEGIN:VEVENT
UID:second@example.com
DTSTART:20260303T090000Z
DTEND:20260303T093000Z
SUMMARY:Second
RRULE:FREQ=WEEKLY;BYDAY=TU
END:VEVENT`)

	assertStarts(t, events,
		"2026-03-02T09:00:00Z",
		"2026-03-03T09:00:00Z",
		"2026-03-09T09:00:00Z",
		"2026-03-10T09:00:00Z",
		"2026-03-16T09:00:00Z",
	)
}

// expandAll decodes the VEVENTs of one iCalendar object and expands them over
// the test window.
func expandAll(t *testing.T, vevents string) []store.Event {
	t.Helper()

	return expandAllBetween(t, vevents, windowStart, windowEnd)
}

func expandAllBetween(t *testing.T, vevents string, from, to time.Time) []store.Event {
	t.Helper()

	document := "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\n" +
		strings.TrimSpace(vevents) + "\nEND:VCALENDAR\n"

	calendar, err := ical.NewDecoder(strings.NewReader(document)).Decode()
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	return expand(calendar, "work", "Team", from, to, zap.NewNop())
}

func assertStarts(t *testing.T, events []store.Event, want ...string) {
	t.Helper()

	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Start.Format(time.RFC3339))
	}

	if !slices.Equal(got, want) {
		t.Errorf("occurrences = %v, want %v", got, want)
	}
}

func date(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}
