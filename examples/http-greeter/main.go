package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Simple Streamable HTTP/SSE MCP server exposing a greet tool at /greeter.
func main() {
	host := flag.String("host", "127.0.0.1", "host to listen on")
	port := flag.String("port", "8081", "port to listen on")
	flag.Parse()

	addr := fmt.Sprintf("%s:%s", *host, *port)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "http-greeter",
		Version: "0.1.0",
	}, nil)

	type args struct {
		Name string `json:"name" jsonschema:"the person to greet"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "greet",
		Description: "say hello to the provided name",
	}, func(ctx context.Context, req *mcp.CallToolRequest, a args) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Hi " + a.Name},
			},
		}, nil, nil
	})

	log.Printf("MCP HTTP greeter listening on %s/greeter", addr)
	handler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		if r.URL.Path == "/greeter" {
			return server
		}
		return nil
	}, nil)

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Printf("http-greeter server failed: %v", err)
		os.Exit(1)
	}
}
