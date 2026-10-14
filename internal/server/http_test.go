package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)

	return t.base.RoundTrip(clone)
}

func TestMCPOverHTTPListsTheTool(t *testing.T) {
	srv := newServer(t, stubEvents{})

	api := httptest.NewServer(srv.router)
	t.Cleanup(api.Close)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(
		context.Background(),
		&mcp.StreamableClientTransport{
			Endpoint: api.URL + "/mcp",
			HTTPClient: &http.Client{Transport: bearerRoundTripper{
				token: "server-key",
				base:  http.DefaultTransport,
			}},
			DisableStandaloneSSE: true,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}

	if len(tools.Tools) != 1 || tools.Tools[0].Name != "get_events" {
		t.Errorf("tools = %v, want just get_events", tools.Tools)
	}
}

func TestMCPRejectsUnauthenticatedRequests(t *testing.T) {
	srv := newServer(t, stubEvents{})

	api := httptest.NewServer(srv.router)
	t.Cleanup(api.Close)

	resp, err := http.Post(api.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
