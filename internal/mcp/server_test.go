package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func TestNewBuildsAServer(t *testing.T) {
	srv, err := New(Deps{Log: zap.NewNop()})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if srv == nil {
		t.Fatal("New() = nil, want a server")
	}
}

func TestServerOverASession(t *testing.T) {
	srv, err := New(Deps{Log: zap.NewNop()})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

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

	info := session.InitializeResult().ServerInfo
	if info.Name != "go-mcp" || info.Version != version {
		t.Errorf("server info = %+v, want go-mcp %s", info, version)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}

	if len(tools.Tools) != 0 {
		t.Errorf("tools = %v, want none until one is registered", tools.Tools)
	}
}
