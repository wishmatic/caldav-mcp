package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

type Deps struct {
	Log *zap.Logger
}

const version = "0.1.0"

func New(deps Deps) (*mcp.Server, error) {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "go-mcp",
		Version: version,
	}, nil)

	registerTools(srv, &handlers{log: deps.Log})

	return srv, nil
}

func registerTools(srv *mcp.Server, h *handlers) {
	// No-op.
}
