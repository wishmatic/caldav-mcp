package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

type Deps struct {
	Log    *zap.Logger
	Events EventSource
}

const version = "0.1.0"

func New(deps Deps) (*mcp.Server, error) {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "caldav-mcp",
		Version: version,
	}, nil)

	registerTools(srv, &handlers{log: deps.Log, events: deps.Events})

	return srv, nil
}

func registerTools(srv *mcp.Server, h *handlers) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_events",
		Description: "List the calendar events in the next duration days, oldest first. " +
			"Only events already fetched into the cache are returned, so a window " +
			"wider than the configured fetch horizon comes back short.",
	}, h.getEvents)
}
