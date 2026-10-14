# CalDAV MCP Specification

This is a CalDAV client that supports any number of CalDAV .ical feeds.

- Authentication over a single Bearer auth token.
- The user supplies URLs in an environment variable CALDAV_SERVERS, with form name=url,name=url.
- Events are cached in an SQLite cache.
- A cron job with a configurable timeout in seconds will fetch events, up to 60 days into the
  future (both values are configurable in envars).
  - This can be done outside of the Go binary but instead in the Dockerfile, as we don't expect
    this ever to be used outside of a Docker deployment.
- This provides exactly one tool call:
  - get_events(duration) with a default of 14 days.
