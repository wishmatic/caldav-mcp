package caldav

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

func TestSyncCachesEventsFromEveryServerItCanReach(t *testing.T) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)

	reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "REPORT" {
			http.NotFound(w, r)

			return
		}

		writeXML(t, w, multistatus(fmt.Sprintf(`
  <D:response>
    <D:href>/cal/one.ics</D:href>
    <D:propstat>
      <D:prop><C:calendar-data>%s</C:calendar-data></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/cal/two.ics</D:href>
    <D:propstat>
      <D:prop><C:calendar-data>%s</C:calendar-data></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`,
			eventDocument(t, "one@example.com", "One", start, ""),
			eventDocument(t, "two@example.com", "Two", start.Add(time.Hour), ""),
		)))
	}))
	t.Cleanup(reachable.Close)

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	t.Cleanup(unreachable.Close)

	servers, err := ParseServers("good=" + reachable.URL + "/cal/,bad=" + unreachable.URL + "/cal/")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	cache := openCache(t)

	// Left behind by an earlier fetch, and finished since.
	if err := cache.Replace(context.Background(), "good", "good", []store.Event{{
		Server: "good", Calendar: "good", UID: "yesterday@example.com",
		Start: start.AddDate(0, 0, -2), End: start.AddDate(0, 0, -1),
	}}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	log := zap.NewNop()
	syncer := NewSyncer(servers, NewFetcher(7*24*time.Hour, log), cache, log)

	err = syncer.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want the unreachable server reported")
	}

	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("Run() error = %v, want it to name the server that failed", err)
	}

	events, err := cache.Between(context.Background(), time.Now(), time.Now().AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("cached events = %+v, want the two from the reachable server", events)
	}

	if events[0].UID != "one@example.com" || events[1].UID != "two@example.com" {
		t.Errorf("cached events = %s, %s, want both of them", events[0].UID, events[1].UID)
	}

	if events[0].Calendar != "good" || events[0].Server != "good" {
		t.Errorf("event = %+v, want it attributed to the server and its calendar", events[0])
	}
}

func TestSyncDropsEventsThatHaveFinished(t *testing.T) {
	cache := openCache(t)

	ctx := context.Background()

	if err := cache.Replace(ctx, "quiet", "quiet", []store.Event{{
		Server: "quiet", Calendar: "quiet", UID: "yesterday@example.com",
		Start: time.Now().AddDate(0, 0, -2), End: time.Now().AddDate(0, 0, -1),
	}}); err != nil {
		t.Fatalf("Replace() error: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/dav", func(w http.ResponseWriter, _ *http.Request) {
		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/dav/</D:href>
    <D:propstat>
      <D:prop><D:current-user-principal><D:href>/principals/user/</D:href></D:current-user-principal></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})
	mux.HandleFunc("/principals/user/", func(w http.ResponseWriter, _ *http.Request) {
		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/principals/user/</D:href>
    <D:propstat>
      <D:prop><C:calendar-home-set><D:href>/dav/calendars/user/</D:href></C:calendar-home-set></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})
	mux.HandleFunc("/dav/calendars/user/", func(w http.ResponseWriter, _ *http.Request) {
		writeXML(t, w, multistatus(`
  <D:response>
    <D:href>/dav/calendars/user/</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	servers, err := ParseServers("quiet=" + server.URL + "/dav/")
	if err != nil {
		t.Fatalf("ParseServers() error: %v", err)
	}

	log := zap.NewNop()

	if err := NewSyncer(servers, NewFetcher(24*time.Hour, log), cache, log).Run(ctx); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	events, err := cache.Between(ctx, time.Now().AddDate(0, 0, -30), time.Now().AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("Between() error: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("cached events = %+v, want the finished one pruned", events)
	}
}

func openCache(t *testing.T) *store.Store {
	t.Helper()

	cache, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "caldav.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	t.Cleanup(func() { _ = cache.Close() })

	return cache
}
