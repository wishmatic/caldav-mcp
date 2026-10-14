# CalDAV MCP

An MCP server that answers questions about your calendars. It fetches events
from any number of CalDAV servers into a SQLite cache and exposes exactly one
tool, `get_events(duration)`, with a default of 14 days.

## Local Development

```sh
API_KEY=change-me \
CALDAV_SERVERS="personal=https://you:app-password@cal.example.com/dav" \
go run ./cmd/server
```

The MCP endpoint is `/mcp`, and `/healthz` answers `ok` without a token.

```sh
docker run -d -p 8080:8080 \
  -v caldav-cache:/data \
  -e API_KEY=change-me \
  -e CALDAV_SERVERS="personal=https://you:app-password@cal.example.com/dav" \
  ghcr.io/wishmatic/caldav-mcp:latest
```

## Configuration

| Variable                | Default           | Meaning                                                                          |
| ----------------------- | ----------------- | -------------------------------------------------------------------------------- |
| `API_KEY`               | none, required    | The bearer token clients must present on `/mcp`.                                 |
| `CALDAV_SERVERS`        | none, required    | `name=url,name=url`. The name identifies the server in the cache and in results. |
| `CACHE_PATH`            | `/data/caldav.db` | The SQLite file the events are cached in.                                        |
| `SYNC_INTERVAL_SECONDS` | `3600`            | How often events are fetched. `0` turns the periodic fetch off.                  |
| `SYNC_LOOKAHEAD_DAYS`   | `60`              | How far ahead events are fetched.                                                |
| `HOST`, `PORT`          | `0.0.0.0`, `8080` | Where the server listens.                                                        |
| `LOG_LEVEL`             | `info`            | Log level.                                                                       |

Credentials go in the URL, as in the example above. Percent-encode anything
reserved in a password, such as `/` or `#`, as in `https://user:a%2Fb@host/dav`;
`@` and `:` need no encoding.

A URL may be:

- a server root, in which case every calendar the account holds is fetched;
- a single calendar collection;
- a published `.ics` feed, such as a subscription URL from Google Calendar or
  Outlook, which is read with a plain GET. Feeds need no credentials, and a
  secret in the query string is passed through untouched.

The URL's shape is worked out from the server's answers, so there is nothing to
configure beyond the URL itself.

## Fetching

A pass fetches every calendar of every server, expands each event's recurrence
rule into individual occurrences inside the lookahead window, and swaps the
result into the cache. Occurrences that have already ended are dropped, so the
cache holds the future and nothing else. `get_events` reads the cache and never
waits on a CalDAV server.

A feed is downloaded whole on each pass and its occurrences expanded from there,
so a feed spanning years costs one download of it per pass and nothing more.

The server fetches on start and then every `SYNC_INTERVAL_SECONDS`. To drive the
fetch from a timer of your own instead, such as a Kubernetes CronJob or a cron
container sharing the cache volume, set `SYNC_INTERVAL_SECONDS=0` and run:

```sh
docker run --rm -v caldav-cache:/data -e ... ghcr.io/wishmatic/caldav-mcp:latest sync
```

## Limitations

- An event's times are expanded in the zone the event names. A rule that crosses
  a daylight saving boundary keeps its wall-clock time, as calendars do.
- Only VEVENTs are fetched; tasks and journal entries are not.
- Results are UTC RFC 3339 timestamps. All-day events keep the date they were
  written with.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
