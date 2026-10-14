package caldav

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestFetchDiscoversCalendars(t *testing.T) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	document := eventDocument(t, "standup@example.com", "Standup", start, "FREQ=WEEKLY;BYDAY=MO")

	var report string

	mux := http.NewServeMux()
	mux.HandleFunc("/dav", func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)

		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/dav/</D:href>
    <D:propstat>
      <D:prop><D:current-user-principal><D:href>/principals/user/</D:href></D:current-user-principal></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})
	mux.HandleFunc("/principals/user/", func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)

		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/principals/user/</D:href>
    <D:propstat>
      <D:prop><C:calendar-home-set><D:href>/dav/calendars/user/</D:href></C:calendar-home-set></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})
	mux.HandleFunc("/dav/calendars/user/", func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)

		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/dav/calendars/user/</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/calendars/user/team/</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype><D:collection/><C:calendar/></D:resourcetype>
        <D:displayname>Team</D:displayname>
        <C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/calendars/user/tasks/</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype><D:collection/><C:calendar/></D:resourcetype>
        <D:displayname>Tasks</D:displayname>
        <C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})
	mux.HandleFunc("/dav/calendars/user/team/", func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)

		body, _ := io.ReadAll(r.Body)
		report = string(body)

		writeXML(t, w, multistatus(fmt.Sprintf(`
  <D:response>
    <D:href>/dav/calendars/user/team/standup.ics</D:href>
    <D:propstat>
      <D:prop><C:calendar-data>%s</C:calendar-data></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, document)))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fetcher := newTestFetcher()

	servers, err := ParseServers("work=" + withCredentials(server.URL) + "/dav/")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	calendars, err := fetcher.Calendars(context.Background(), servers[0])
	if err != nil {
		t.Fatalf("Calendars() error: %v", err)
	}

	if len(calendars) != 1 {
		t.Fatalf("calendars = %+v, want only the one holding events", calendars)
	}

	if calendars[0].Name != "Team" || calendars[0].Path != "/dav/calendars/user/team/" {
		t.Errorf("calendar = %+v, want the team calendar", calendars[0])
	}

	events, err := fetcher.Events(context.Background(), servers[0], calendars[0])
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("events = none, want the recurring event expanded")
	}

	if events[0].Summary != "Standup" || events[0].Calendar != "Team" || events[0].Server != "work" {
		t.Errorf("event = %+v, want it read from the team calendar", events[0])
	}

	assertQueryWindow(t, report)
}

func TestFetchFallsBackToTheURLAAsACalendar(t *testing.T) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	document := eventDocument(t, "dentist@example.com", "Dentist", start, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/dav/calendars/user/personal/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "REPORT" {
			// Nothing to discover here: this URL is the calendar itself.
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		writeXML(t, w, multistatus(fmt.Sprintf(`
  <D:response>
    <D:href>/dav/calendars/user/personal/dentist.ics</D:href>
    <D:propstat>
      <D:prop><C:calendar-data>%s</C:calendar-data></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, document)))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	fetcher := newTestFetcher()

	servers, err := ParseServers("personal=" + server.URL + "/dav/calendars/user/personal")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	calendars, err := fetcher.Calendars(context.Background(), servers[0])
	if err != nil {
		t.Fatalf("Calendars() error: %v", err)
	}

	if len(calendars) != 1 || calendars[0].Path != "/dav/calendars/user/personal/" {
		t.Fatalf("calendars = %+v, want the URL read as one calendar collection", calendars)
	}

	if calendars[0].Name != "personal" {
		t.Errorf("name = %q, want the server's name to stand in", calendars[0].Name)
	}

	events, err := fetcher.Events(context.Background(), servers[0], calendars[0])
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}

	if len(events) != 1 || events[0].Summary != "Dentist" {
		t.Errorf("events = %+v, want the one event", events)
	}
}

// assertQueryWindow checks the REPORT asked for VEVENTs in the window the
// fetcher was given: from now, sixty days out.
func assertQueryWindow(t *testing.T, report string) {
	t.Helper()

	if !strings.Contains(report, `name="VEVENT"`) {
		t.Errorf("query = %s, want a VEVENT filter", report)
	}

	window := timeRange.FindStringSubmatch(report)
	if window == nil {
		t.Fatalf("query = %s, want a time range", report)
	}

	from, err := time.Parse("20060102T150405Z", window[1])
	if err != nil {
		t.Fatalf("time range start %q: %v", window[1], err)
	}

	to, err := time.Parse("20060102T150405Z", window[2])
	if err != nil {
		t.Fatalf("time range end %q: %v", window[2], err)
	}

	if elapsed := time.Since(from); elapsed < 0 || elapsed > time.Minute {
		t.Errorf("window starts %v ago, want it to start now", elapsed)
	}

	if lookahead := to.Sub(from); lookahead != 60*24*time.Hour {
		t.Errorf("window = %v, want the configured 60 days", lookahead)
	}
}

var timeRange = regexp.MustCompile(`<time-range[^>]*start="([^"]+)"[^>]*end="([^"]+)"`)

func newTestFetcher() *Fetcher {
	return NewFetcher(60*24*time.Hour, zap.NewNop())
}

// withCredentials puts an account in the URL, which is how a CalDAV server is
// told who is asking.
func withCredentials(url string) string {
	return strings.Replace(url, "://", "://user:secret@", 1)
}

// requireBasicAuth stands in for the CalDAV account the credentials in the URL
// are meant for.
func requireBasicAuth(t *testing.T, r *http.Request) {
	t.Helper()

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want the credentials from the URL", got)
	}
}

func writeXML(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	if _, err := w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>` + body)); err != nil {
		t.Errorf("Write() error: %v", err)
	}
}

func multistatus(responses string) string {
	return `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` + responses + `</D:multistatus>`
}

func eventDocument(t *testing.T, uid, summary string, start time.Time, rule string) string {
	t.Helper()

	document := "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\nBEGIN:VEVENT\n" +
		"UID:" + uid + "\n" +
		"DTSTART:" + start.Format("20060102T150405Z") + "\n" +
		"DTEND:" + start.Add(time.Hour).Format("20060102T150405Z") + "\n" +
		"SUMMARY:" + summary + "\n"

	if rule != "" {
		document += "RRULE:" + rule + "\n"
	}

	return document + "END:VEVENT\nEND:VCALENDAR"
}
