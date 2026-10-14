package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wishmatic/go-mcp/internal/auth"
	"github.com/wishmatic/go-mcp/internal/config"
	"go.uber.org/zap"
)

func TestShutdown(t *testing.T) {
	srv, err := New(config.Config{Port: 8080, APIKey: "server-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New(config.Config{Port: 8080}, zap.NewNop())
	if !errors.Is(err, auth.ErrNoAPIKey) {
		t.Errorf("New() error = %v, want auth.ErrNoAPIKey", err)
	}
}

func TestNewRejectsAnInvalidPort(t *testing.T) {
	_, err := New(config.Config{APIKey: "server-key"}, zap.NewNop())
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("New() error = %v, want it to name PORT", err)
	}
}

func TestHealthz(t *testing.T) {
	srv, err := New(config.Config{Port: 8080, APIKey: "server-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a bearer token", rec.Code)
	}

	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rec.Body.String())
	}
}

func TestMCPRejectsBadCredentials(t *testing.T) {
	srv, err := New(config.Config{Port: 8080, APIKey: "server-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	authHeaders := map[string]string{
		"no header":    "",
		"wrong token":  "Bearer nope",
		"wrong scheme": "Basic server-key",
	}

	for name, authHeader := range authHeaders {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
			if authHeader != "" {
				req.Header.Set("Authorization", authHeader)
			}

			rec := httptest.NewRecorder()
			srv.router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestMCPAcceptsTheConfiguredToken(t *testing.T) {
	srv, err := New(config.Config{Port: 8080, APIKey: "server-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer server-key")

	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Error("status = 401, want the request to reach the MCP handler")
	}
}
