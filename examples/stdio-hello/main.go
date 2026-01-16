package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A minimal stdio MCP server exposing a single greet tool.
func main() {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "stdio-hello",
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

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("stdio-hello server failed: %v", err)
	}
}
