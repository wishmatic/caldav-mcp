// Package caldav fetches events from the configured CalDAV servers and keeps
// the SQLite cache in step with them.
package caldav

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

// requestTimeout bounds one CalDAV request. A fetch pass is allowed to take as
// long as its calendars need, but a single stalled request cannot hold it up
// for ever.
const requestTimeout = 60 * time.Second

// maxFeedSize bounds a published .ics feed. A feed of a few megabytes is
// ordinary; past this something is wrong with the URL.
const maxFeedSize = 32 << 20

// icalDocumentHeader is what every iCalendar document starts with, which
// identifies a feed even when the server labels the response vaguely.
const icalDocumentHeader = "BEGIN:VCALENDAR"

// Fetcher reads events from CalDAV servers.
type Fetcher struct {
	http      webdav.HTTPClient
	lookahead time.Duration
	log       *zap.Logger
}

func NewFetcher(lookahead time.Duration, log *zap.Logger) *Fetcher {
	return &Fetcher{
		http:      &http.Client{Timeout: requestTimeout},
		lookahead: lookahead,
		log:       log,
	}
}

// Calendar is one calendar collection on a server, or one published .ics feed.
// Path locates it, Name is what it calls itself.
type Calendar struct {
	Path string
	Name string
	// Feed marks a URL that serves a single iCalendar document rather than a
	// CalDAV collection, so it is read with a plain GET.
	Feed bool
}

// Calendars lists the calendars a server holds events in.
func (f *Fetcher) Calendars(ctx context.Context, srv Server) ([]Calendar, error) {
	client, err := f.client(srv)
	if err != nil {
		return nil, err
	}

	calendars, err := discover(ctx, client)
	if err == nil {
		return calendars, nil
	}

	// A URL that points straight at a calendar collection or at a published
	// .ics feed has no home set to discover, so read it as whichever it is.
	f.log.Debug("calendar discovery failed, reading the URL on its own",
		zap.String("server", srv.Name), zap.Error(err))

	calendar := Calendar{Path: collectionPath(srv.URL), Name: srv.Name}

	// What the URL answers with is the only thing that identifies a feed. The
	// address itself says nothing: a feed has no extension to go by, and a
	// link copied out of a calendar service often ends in .html.
	served, contentType := f.servesCalendar(ctx, srv)

	if served {
		calendar.Feed = true

		f.log.Debug("reading the URL as a published .ics feed", zap.String("server", srv.Name))
	} else {
		f.log.Debug("reading the URL as a CalDAV calendar collection",
			zap.String("server", srv.Name), zap.String("content_type", contentType))
	}

	return []Calendar{calendar}, nil
}

// Events returns every occurrence the calendar holds inside the fetch window.
func (f *Fetcher) Events(ctx context.Context, srv Server, cal Calendar) ([]store.Event, error) {
	from, to := f.window()

	if cal.Feed {
		return f.feedEvents(ctx, srv, cal, from, to)
	}

	client, err := f.client(srv)
	if err != nil {
		return nil, err
	}

	objects, err := client.QueryCalendar(ctx, cal.Path, &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:  ical.CompCalendar,
			Comps: []caldav.CalendarCompRequest{{Name: ical.CompEvent, AllProps: true}},
		},
		CompFilter: caldav.CompFilter{
			Name:  ical.CompCalendar,
			Comps: []caldav.CompFilter{{Name: ical.CompEvent, Start: from, End: to}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", cal.Path, err)
	}

	var events []store.Event
	for _, object := range objects {
		if object.Data == nil {
			continue
		}

		events = append(events, expand(object.Data, srv.Name, cal.Name, from, to, f.log)...)
	}

	return events, nil
}

// servesCalendar reports whether the URL answers with an iCalendar document,
// along with the content type it answered with, which says why when it does
// not.
func (f *Fetcher) servesCalendar(ctx context.Context, srv Server) (bool, string) {
	response, err := f.request(ctx, srv.URL)
	if err != nil {
		return false, "unreachable"
	}

	defer func() { _ = response.Body.Close() }()

	contentType := response.Header.Get("Content-Type")

	if response.StatusCode != http.StatusOK {
		return false, response.Status
	}

	if strings.Contains(contentType, "text/calendar") {
		return true, contentType
	}

	head, err := io.ReadAll(io.LimitReader(response.Body, int64(len(icalDocumentHeader))))
	if err != nil {
		return false, contentType
	}

	return string(head) == icalDocumentHeader, contentType
}

// feedEvents reads a published .ics feed in one request and expands it over the
// window, exactly as it would expand a calendar object.
func (f *Fetcher) feedEvents(ctx context.Context, srv Server, cal Calendar, from, to time.Time) ([]store.Event, error) {
	response, err := f.request(ctx, srv.URL)
	if err != nil {
		return nil, err
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", redactURL(srv.URL), response.Status)
	}

	document, err := io.ReadAll(io.LimitReader(response.Body, maxFeedSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", redactURL(srv.URL), err)
	}

	if len(document) > maxFeedSize {
		return nil, fmt.Errorf("read %s: the feed is larger than %d bytes", redactURL(srv.URL), maxFeedSize)
	}

	calendar, err := ical.NewDecoder(bytes.NewReader(document)).Decode()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", redactURL(srv.URL), err)
	}

	return expand(calendar, srv.Name, cal.Name, from, to, f.log), nil
}

// request starts a GET, which is all a published .ics feed asks for. The
// credentials in the URL go out as basic auth, as net/http arranges.
func (f *Fetcher) request(ctx context.Context, feed *url.URL) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build a request for %s: %w", redactURL(feed), err)
	}

	response, err := f.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", redactURL(feed), err)
	}

	return response, nil
}

// redactURL is the URL as it is safe to log: a feed carries its secret in the
// query string as often as a CalDAV server carries one in the userinfo.
func redactURL(u *url.URL) string {
	redacted := *u
	if redacted.RawQuery != "" {
		redacted.RawQuery = "redacted"
	}

	return redacted.Redacted()
}

// window is the span a fetch covers: from now to now plus the lookahead. Both
// ends are UTC because the CalDAV client writes a time range out with a literal
// Z and no conversion, so a local time would go to the server shifted by the
// process's own offset.
func (f *Fetcher) window() (time.Time, time.Time) {
	now := time.Now().UTC()

	return now, now.Add(f.lookahead)
}

func (f *Fetcher) client(srv Server) (*caldav.Client, error) {
	client, err := caldav.NewClient(f.http, srv.URL.String())
	if err != nil {
		return nil, fmt.Errorf("client for %s: %w", srv.Name, err)
	}

	return client, nil
}

func discover(ctx context.Context, client *caldav.Client) ([]Calendar, error) {
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, fmt.Errorf("find the current user principal: %w", err)
	}

	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("find the calendar home set: %w", err)
	}

	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	found := make([]Calendar, 0, len(calendars))
	for _, calendar := range calendars {
		// A collection that only promises tasks or journal entries has nothing
		// for a calendar view to show.
		if !holdsEvents(calendar) {
			continue
		}

		found = append(found, Calendar{Path: calendar.Path, Name: calendarName(calendar)})
	}

	return found, nil
}

// holdsEvents reports whether a collection advertises VEVENT support. Servers
// are allowed to leave the component set out, which is read as "anything".
func holdsEvents(calendar caldav.Calendar) bool {
	if len(calendar.SupportedComponentSet) == 0 {
		return true
	}

	for _, component := range calendar.SupportedComponentSet {
		if component == ical.CompEvent {
			return true
		}
	}

	return false
}

func calendarName(calendar caldav.Calendar) string {
	if calendar.Name != "" {
		return calendar.Name
	}

	return calendar.Path
}

// collectionPath is the request path for a URL that is itself a calendar: the
// collection, with the trailing slash servers expect.
func collectionPath(u *url.URL) string {
	path := u.Path
	if path == "" {
		path = "/"
	}

	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	return path
}
