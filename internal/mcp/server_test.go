package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

type fakeEvents struct {
	events   []store.Event
	err      error
	from, to time.Time
}

func (f *fakeEvents) Between(_ context.Context, from, to time.Time) ([]store.Event, error) {
	f.from, f.to = from, to

	return f.events, f.err
}

func TestNewBuildsAServer(t *testing.T) {
	srv, err := New(Deps{Log: zap.NewNop(), Events: &fakeEvents{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if srv == nil {
		t.Fatal("New() = nil, want a server")
	}
}

func TestServerOverASession(t *testing.T) {
	srv, err := New(Deps{Log: zap.NewNop(), Events: &fakeEvents{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	session := connect(t, srv)

	info := session.InitializeResult().ServerInfo
	if info.Name != "caldav-mcp" || info.Version != version {
		t.Errorf("server info = %+v, want caldav-mcp %s", info, version)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}

	if len(tools.Tools) != 1 {
		t.Fatalf("tools = %v, want exactly one", tools.Tools)
	}

	if name := tools.Tools[0].Name; name != "get_events" {
		t.Errorf("tool name = %q, want get_events", name)
	}
}

func TestServerServesGetEvents(t *testing.T) {
	source := &fakeEvents{events: []store.Event{{
		UID:         "standup@example.com",
		Start:       time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second),
		End:         time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second),
		Summary:     "Standup",
		Description: "Daily sync",
		Location:    "Kitchen",
		AllDay:      false,
		Calendar:    "Personal",
		Server:      "personal",
	}}}

	srv, err := New(Deps{Log: zap.NewNop(), Events: source})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	session := connect(t, srv)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_events",
		Arguments: map[string]any{"duration": 7},
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if result.IsError {
		t.Fatalf("CallTool() = %v, want a result", result.Content)
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var output getEventsOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if len(output.Events) != 1 {
		t.Fatalf("events = %v, want the one cached event", output.Events)
	}

	if got := output.Events[0].Summary; got != "Standup" {
		t.Errorf("summary = %q, want Standup", got)
	}

	if output.Events[0].AllDay {
		t.Error("allDay = true, want false")
	}

	requested, err := time.Parse(time.RFC3339, output.To)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if days := requested.Sub(source.from).Hours() / 24; days < 6.99 || days > 7.01 {
		t.Errorf("window = %v days, want the requested 7", days)
	}
}

func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error: %v", err)
	}

	t.Cleanup(func() { _ = serverSession.Close() })

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(
		ctx, clientTransport, nil,
	)
	if err != nil {
		t.Fatalf("client Connect() error: %v", err)
	}

	t.Cleanup(func() { _ = session.Close() })

	return session
}
