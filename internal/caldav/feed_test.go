package caldav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchReadsAPublishedICSFeed(t *testing.T) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	document := eventDocument(t, "standup@example.com", "Standup", start, "FREQ=WEEKLY;BYDAY=MO")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)

		// Discovery is tried first, and it drops the query string; the reads
		// that follow are the ones that have to carry the token.
		if r.Method == http.MethodGet && r.URL.RawQuery != "token=s3cret" {
			t.Errorf("query = %q, want the token in the URL kept", r.URL.RawQuery)
		}

		// A published feed answers every method with the same document.
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = w.Write([]byte(document))
	}))
	t.Cleanup(server.Close)

	servers, err := ParseServers("feed=" + withCredentials(server.URL) + "/calendar.ics?token=s3cret")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	calendars, err := newTestFetcher().Calendars(context.Background(), servers[0])
	if err != nil {
		t.Fatalf("Calendars() error: %v", err)
	}

	if len(calendars) != 1 || !calendars[0].Feed {
		t.Fatalf("calendars = %+v, want the URL read as one feed", calendars)
	}

	if calendars[0].Name != "feed" {
		t.Errorf("name = %q, want the server's name", calendars[0].Name)
	}

	events, err := newTestFetcher().Events(context.Background(), servers[0], calendars[0])
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}

	if len(events) < 2 {
		t.Fatalf("events = %+v, want the weekly rule expanded over the window", events)
	}

	for _, event := range events {
		if event.UID != "standup@example.com" || event.Summary != "Standup" {
			t.Errorf("event = %+v, want it read from the feed", event)
		}
	}
}

// A feed URL names nothing useful: no .ics, a query string, or a bare path.
// What identifies the feed is the response, never the address.
func TestFetchReadsAFeedWhateverTheURLIsCalled(t *testing.T) {
	document := eventDocument(t, "standup@example.com", "Standup",
		time.Now().UTC().Add(24*time.Hour).Truncate(time.Second), "")

	tests := map[string]struct {
		path        string
		contentType string
	}{
		"a bare path":            {path: "/feed", contentType: "text/calendar"},
		"an opaque id":           {path: "/api/calendar/6f3a9b21", contentType: "text/calendar; charset=utf-8"},
		"a vague content type":   {path: "/api/calendar/6f3a9b21", contentType: "application/octet-stream"},
		"no content type at all": {path: "/api/calendar/6f3a9b21", contentType: ""},
		"a query string":         {path: "/api/calendar?export=1&token=s3cret", contentType: "text/plain"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path, _, _ := strings.Cut(tt.path, "?")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path {
					http.NotFound(w, r)

					return
				}

				if tt.contentType != "" {
					w.Header().Set("Content-Type", tt.contentType)
				}

				_, _ = w.Write([]byte(document))
			}))
			t.Cleanup(server.Close)

			servers, err := ParseServers("feed=" + server.URL + tt.path)
			if err != nil {
				t.Fatalf("ParseServers() error: %v", err)
			}

			calendars, err := newTestFetcher().Calendars(context.Background(), servers[0])
			if err != nil {
				t.Fatalf("Calendars() error: %v", err)
			}

			if len(calendars) != 1 || !calendars[0].Feed {
				t.Fatalf("calendars = %+v, want the URL read as one feed", calendars)
			}

			events, err := newTestFetcher().Events(context.Background(), servers[0], calendars[0])
			if err != nil {
				t.Fatalf("Events() error: %v", err)
			}

			if len(events) != 1 || events[0].Summary != "Standup" {
				t.Errorf("events = %+v, want the one event", events)
			}
		})
	}
}

func TestFetchFollowsARedirectToAFeed(t *testing.T) {
	document := eventDocument(t, "holiday@example.com", "Holiday",
		time.Now().UTC().Add(48*time.Hour).Truncate(time.Second), "")

	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/feed/6f3a9b21", http.StatusFound)
	})
	mux.HandleFunc("/feed/6f3a9b21", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(document))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	servers, err := ParseServers("feed=" + server.URL + "/subscribe")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	calendars, err := newTestFetcher().Calendars(context.Background(), servers[0])
	if err != nil {
		t.Fatalf("Calendars() error: %v", err)
	}

	if len(calendars) != 1 || !calendars[0].Feed {
		t.Fatalf("calendars = %+v, want the redirect followed to the feed", calendars)
	}

	events, err := newTestFetcher().Events(context.Background(), servers[0], calendars[0])
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}

	if len(events) != 1 || events[0].Summary != "Holiday" {
		t.Errorf("events = %+v, want the one event", events)
	}
}

func TestFetchReportsAFeedItCannotRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		http.Error(w, "gone", http.StatusGone)
	}))
	t.Cleanup(server.Close)

	servers, err := ParseServers("feed=" + server.URL + "/calendar.ics")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	calendars, err := newTestFetcher().Calendars(context.Background(), servers[0])
	if err != nil {
		t.Fatalf("Calendars() error: %v", err)
	}

	// The feed is only claimed once a GET has read it, and this one answers
	// with an error status, so the URL falls back to a calendar collection and
	// the failure is reported when it is read.
	if _, err := newTestFetcher().Events(context.Background(), servers[0], calendars[0]); err == nil {
		t.Error("Events() error = nil, want the 410 reported")
	}
}

func TestRedactURLKeepsSecretsOutOfLogs(t *testing.T) {
	feed, err := url.Parse("https://user:secret@host.example.com/feed.ics?token=s3cret")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	got := redactURL(feed)

	for _, secret := range []string{"secret", "s3cret"} {
		if strings.Contains(got, secret) {
			t.Errorf("redactURL() = %q, want %q kept out of it", got, secret)
		}
	}

	if !strings.Contains(got, "host.example.com/feed.ics") {
		t.Errorf("redactURL() = %q, want the address still readable", got)
	}
}
