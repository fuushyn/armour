# Example MCP servers

These examples give you quick, known-good MCP servers to point the proxy (or any MCP client) at when testing transports and capability negotiation.

- **stdio-hello** — Minimal stdio server with a single `greet` tool.
  - Run: `go run ./examples/stdio-hello`
  - Expectation: speaks MCP over stdin/stdout; useful for local stdio transport checks.

- **http-greeter** — SSE/HTTP server with a single `greet` tool exposed at `/greeter`.
  - Run: `go run ./examples/http-greeter --host 127.0.0.1 --port 8081`
  - Expectation: speaks Streamable HTTP; good for session/headers/SSE reconnection exercises.

Both servers are intentionally tiny and rely only on the official `github.com/modelcontextprotocol/go-sdk`. They’re safe defaults for manual experiments or automated proxy smoke tests.
