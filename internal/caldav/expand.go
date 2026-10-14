package caldav

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

// dateLength is the length of an iCalendar DATE value, as opposed to a
// DATE-TIME, which is what tells an all-day event from a timed one.
const dateLength = len("20060102")

// vevent is the part of a VEVENT this package cares about.
type vevent struct {
	uid          string
	start        time.Time
	end          time.Time
	allDay       bool
	summary      string
	description  string
	location     string
	cancelled    bool
	recurrenceID time.Time
	recurrence   string
	component    ical.Event
}

// expand turns one iCalendar object into the occurrences the cache stores. An
// object holds a whole recurrence set: the master VEVENT plus, for every
// instance that was moved or edited, an override carrying a RECURRENCE-ID.
func expand(cal *ical.Calendar, server, calendar string, from, to time.Time, log *zap.Logger) []store.Event {
	masters := make(map[string]vevent)
	overrides := make(map[string][]vevent)

	for _, component := range cal.Events() {
		event, ok := parseEvent(component, log)
		if !ok {
			continue
		}

		if event.recurrenceID.IsZero() {
			masters[event.uid] = event
		} else {
			overrides[event.uid] = append(overrides[event.uid], event)
		}
	}

	var out []store.Event

	for _, uid := range seriesUIDs(masters, overrides) {
		if master, ok := masters[uid]; ok && !master.cancelled {
			out = append(out, master.occurrences(overrides[uid], server, calendar, from, to, log)...)
		}

		// An override stands on its own: it may have been moved into the
		// window, or out of it, without the master knowing.
		for _, override := range overrides[uid] {
			if override.cancelled || !overlaps(override.start, override.end, from, to) {
				continue
			}

			out = append(out, override.cached(server, calendar))
		}
	}

	slices.SortFunc(out, func(a, b store.Event) int {
		return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.UID, b.UID))
	})

	return out
}

func seriesUIDs(masters map[string]vevent, overrides map[string][]vevent) []string {
	uidSet := make(map[string]bool, len(masters)+len(overrides))
	for uid := range masters {
		uidSet[uid] = true
	}

	for uid := range overrides {
		uidSet[uid] = true
	}

	uids := make([]string, 0, len(uidSet))
	for uid := range uidSet {
		uids = append(uids, uid)
	}

	slices.Sort(uids)

	return uids
}

// occurrences expands a master event, leaving out the instances an override
// has taken over.
func (v vevent) occurrences(overrides []vevent, server, calendar string, from, to time.Time, log *zap.Logger) []store.Event {
	duration := v.end.Sub(v.start)

	replaced := make(map[int64]bool, len(overrides))
	for _, override := range overrides {
		replaced[override.recurrenceID.Unix()] = true
	}

	// The rule only yields starts, so an event long enough to reach into the
	// window while starting before it has to be searched for from earlier.
	starts, err := recurrenceStarts(v, from.Add(-duration), to)
	if err != nil {
		log.Warn("skipping an event whose recurrence rule cannot be read",
			zap.String("uid", v.uid), zap.Error(err))

		return nil
	}

	single := starts == nil
	if single {
		starts = []time.Time{v.start}
	}

	var out []store.Event

	for _, start := range starts {
		if replaced[start.Unix()] {
			continue
		}

		end := start.Add(duration)
		if !overlaps(start, end, from, to) {
			continue
		}

		occurrence := v
		occurrence.start, occurrence.end = start, end

		if !single {
			occurrence.recurrence = start.UTC().Format(time.RFC3339)
		}

		out = append(out, occurrence.cached(server, calendar))
	}

	return out
}

func (v vevent) cached(server, calendar string) store.Event {
	return store.Event{
		Server:      server,
		Calendar:    calendar,
		UID:         v.uid,
		Recurrence:  v.recurrence,
		Start:       v.start.UTC(),
		End:         v.end.UTC(),
		AllDay:      v.allDay,
		Summary:     v.summary,
		Description: v.description,
		Location:    v.location,
	}
}

// recurrenceStarts returns the start of every occurrence in [from, to]. A nil
// slice with a nil error means the event does not recur at all.
func recurrenceStarts(v vevent, from, to time.Time) ([]time.Time, error) {
	rules := v.component.Props[ical.PropRecurrenceRule]
	rdates := v.component.Props[ical.PropRecurrenceDates]
	exdates := v.component.Props[ical.PropExceptionDates]

	if len(rules) == 0 && len(rdates) == 0 && len(exdates) == 0 {
		return nil, nil
	}

	var set rrule.Set

	if len(rules) == 0 {
		// With no rule the set is DTSTART plus the RDATEs, and the set iterator
		// only knows about the latter.
		set.RDate(v.start)
	} else {
		option, err := rrule.StrToROption(rules[0].Value)
		if err != nil {
			return nil, fmt.Errorf("parse RRULE %q: %w", rules[0].Value, err)
		}

		rule, err := rrule.NewRRule(*option)
		if err != nil {
			return nil, fmt.Errorf("build RRULE %q: %w", rules[0].Value, err)
		}

		set.RRule(rule)
	}

	// DTSTART goes last: rrule forwards it to the rule that is already set.
	set.DTStart(v.start)

	for _, prop := range rdates {
		dates, err := parseDateList(&prop)
		if err != nil {
			return nil, fmt.Errorf("parse RDATE %q: %w", prop.Value, err)
		}

		for _, date := range dates {
			set.RDate(date)
		}
	}

	for _, prop := range exdates {
		dates, err := parseDateList(&prop)
		if err != nil {
			return nil, fmt.Errorf("parse EXDATE %q: %w", prop.Value, err)
		}

		for _, date := range dates {
			set.ExDate(date)
		}
	}

	return set.Between(from, to, true), nil
}

func parseEvent(component ical.Event, log *zap.Logger) (vevent, bool) {
	uid := text(component, ical.PropUID)
	if uid == "" {
		log.Warn("skipping an event with no UID; without one its occurrences cannot be told apart")

		return vevent{}, false
	}

	startProp := component.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		log.Warn("skipping an event with no DTSTART", zap.String("uid", uid))

		return vevent{}, false
	}

	start, err := parseDate(startProp)
	if err != nil {
		log.Warn("skipping an event with an unreadable DTSTART", zap.String("uid", uid), zap.Error(err))

		return vevent{}, false
	}

	allDay := startProp.ValueType() == ical.ValueDate || len(startProp.Value) == dateLength

	end, err := parseEventEnd(component, start, allDay)
	if err != nil {
		log.Warn("skipping an event with an unreadable DTEND", zap.String("uid", uid), zap.Error(err))

		return vevent{}, false
	}

	if end.Before(start) {
		end = start
	}

	event := vevent{
		uid:         uid,
		start:       start,
		end:         end,
		allDay:      allDay,
		summary:     text(component, ical.PropSummary),
		description: text(component, ical.PropDescription),
		location:    text(component, ical.PropLocation),
		component:   component,
	}

	if prop := component.Props.Get(ical.PropRecurrenceID); prop != nil {
		id, err := parseDate(prop)
		if err != nil {
			log.Warn("skipping an event with an unreadable RECURRENCE-ID",
				zap.String("uid", uid), zap.Error(err))

			return vevent{}, false
		}

		event.recurrenceID = id
		event.recurrence = id.UTC().Format(time.RFC3339)
	}

	if status, err := component.Status(); err == nil && status == ical.EventCancelled {
		event.cancelled = true
	}

	return event, true
}

func parseEventEnd(component ical.Event, start time.Time, allDay bool) (time.Time, error) {
	if prop := component.Props.Get(ical.PropDateTimeEnd); prop != nil {
		return parseDate(prop)
	}

	if prop := component.Props.Get(ical.PropDuration); prop != nil {
		duration, err := prop.Duration()
		if err != nil {
			return time.Time{}, err
		}

		return start.Add(duration), nil
	}

	// DTEND is optional, and its absence means different things per RFC 5545:
	// a day for an all-day event, nothing at all for a timed one.
	if allDay {
		return start.AddDate(0, 0, 1), nil
	}

	return start, nil
}

// parseDate reads a DATE or DATE-TIME property. go-ical resolves a TZID
// through the zone database and gives up on names it cannot find, so a value
// carrying an unknown zone falls back to being read as UTC.
func parseDate(prop *ical.Prop) (time.Time, error) {
	parsed, err := prop.DateTime(time.UTC)
	if err == nil {
		return parsed, nil
	}

	for _, layout := range []string{time.RFC3339, "20060102T150405Z", "20060102T150405", "20060102"} {
		if at, parseErr := time.ParseInLocation(layout, prop.Value, time.UTC); parseErr == nil {
			return at, nil
		}
	}

	return time.Time{}, err
}

// parseDateList reads a property whose value is a comma-separated list, which
// is how EXDATE and RDATE carry more than one value.
func parseDateList(prop *ical.Prop) ([]time.Time, error) {
	var out []time.Time

	for _, value := range strings.Split(prop.Value, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		// The copy keeps the TZID and VALUE parameters the value was written with.
		copied := ical.Prop{Name: prop.Name, Value: value, Params: prop.Params}

		date, err := parseDate(&copied)
		if err != nil {
			return nil, err
		}

		out = append(out, date)
	}

	return out, nil
}

func text(component ical.Event, name string) string {
	prop := component.Props.Get(name)
	if prop == nil {
		return ""
	}

	return unescapeText(prop.Value)
}

// unescapeText decodes an iCalendar TEXT value. go-ical reads a comma as the
// separator between values and returns only the first, which cuts off the many
// summaries, descriptions and locations that producers write with an
// unescaped comma in them.
func unescapeText(value string) string {
	var out strings.Builder

	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 == len(value) {
			out.WriteByte(value[i])

			continue
		}

		i++

		switch value[i] {
		case 'n', 'N':
			out.WriteByte('\n')
		default:
			out.WriteByte(value[i])
		}
	}

	return out.String()
}

// overlaps reports whether [start, end] shares any time with [from, to].
func overlaps(start, end, from, to time.Time) bool {
	return !end.Before(from) && !start.After(to)
}
