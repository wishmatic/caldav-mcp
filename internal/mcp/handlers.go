package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/wishmatic/caldav-mcp/internal/store"
)

// defaultDurationDays is the window get_events answers for when the caller does
// not say.
const defaultDurationDays = 14

// EventSource is the cached events the get_events tool reads.
type EventSource interface {
	Between(ctx context.Context, from, to time.Time) ([]store.Event, error)
}

type handlers struct {
	log    *zap.Logger
	events EventSource
}

type getEventsInput struct {
	Duration int `json:"duration,omitempty" jsonschema:"how many days ahead to look; defaults to 14"`
}

type getEventsOutput struct {
	From   string  `json:"from" jsonschema:"start of the window, RFC 3339"`
	To     string  `json:"to" jsonschema:"end of the window, RFC 3339"`
	Events []event `json:"events" jsonschema:"events in the window, oldest first"`
}

type event struct {
	ID          string `json:"id" jsonschema:"the event's iCalendar UID, shared by every occurrence of a recurring event"`
	Summary     string `json:"summary"`
	Start       string `json:"start" jsonschema:"RFC 3339"`
	End         string `json:"end" jsonschema:"RFC 3339"`
	AllDay      bool   `json:"allDay" jsonschema:"the event covers whole days, so only the dates matter"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	Calendar    string `json:"calendar"`
	Server      string `json:"server"`
	Recurrence  string `json:"recurrence,omitempty" jsonschema:"for one occurrence of a recurring event, the original start of that occurrence, RFC 3339"`
}

func (h *handlers) getEvents(ctx context.Context, _ *mcp.CallToolRequest, input getEventsInput) (*mcp.CallToolResult, getEventsOutput, error) {
	days := input.Duration
	if days <= 0 {
		days = defaultDurationDays
	}

	from := time.Now()
	to := from.AddDate(0, 0, days)

	cached, err := h.events.Between(ctx, from, to)
	if err != nil {
		return nil, getEventsOutput{}, fmt.Errorf("read the event cache: %w", err)
	}

	output := getEventsOutput{
		From:   from.UTC().Format(time.RFC3339),
		To:     to.UTC().Format(time.RFC3339),
		Events: make([]event, 0, len(cached)),
	}

	for _, cachedEvent := range cached {
		output.Events = append(output.Events, event{
			ID:          cachedEvent.UID,
			Summary:     cachedEvent.Summary,
			Start:       cachedEvent.Start.Format(time.RFC3339),
			End:         cachedEvent.End.Format(time.RFC3339),
			AllDay:      cachedEvent.AllDay,
			Location:    cachedEvent.Location,
			Description: cachedEvent.Description,
			Calendar:    cachedEvent.Calendar,
			Server:      cachedEvent.Server,
			Recurrence:  cachedEvent.Recurrence,
		})
	}

	return nil, output, nil
}
