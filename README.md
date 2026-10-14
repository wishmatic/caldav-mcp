# go-mcp

A template for an MCP server written in Go.

## Local Development

```sh
API_KEY=change-me go run ./cmd/server
```

The MCP endpoint is `/mcp`, and `/healthz` answers `ok` without a token.

```sh
docker run -d -p 8080:8080 -e API_KEY=change-me ghcr.io/wishmatic/go-mcp:latest
```

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
