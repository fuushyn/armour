//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/mcp-go-proxy/cmd"
	"github.com/user/mcp-go-proxy/proxy"
	"github.com/user/mcp-go-proxy/server"
)

func TestDaemonIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"ok":true}}`))
	}))
	defer upstream.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")
	configJSON := fmt.Sprintf(`{
  "servers": [
    {"name": "test", "transport": "http", "url": "%s"}
  ]
}`, upstream.URL)
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := cmd.CLIArgs{
		ListenAddr: "127.0.0.1:0",
		ConfigPath: configPath,
		Mode:       "http",
		LogLevel:   "info",
	}

	config := cmd.CLIArgsToServerConfig(args)

	srv, err := server.NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx)
	time.Sleep(200 * time.Millisecond)

	actualAddr := srv.GetListenAddr()

	client := &http.Client{}

	healthTests := []struct {
		name     string
		method   string
		endpoint string
		headers  map[string]string
		expectOK bool
	}{
		{
			"health check",
			http.MethodGet,
			"/healthz",
			nil,
			true,
		},
		{
			"MCP endpoint requires session",
			http.MethodPost,
			"/mcp",
			nil,
			false,
		},
		{
			"MCP endpoint with session",
			http.MethodPost,
			"/mcp",
			map[string]string{
				proxy.HeaderSessionID: "test-session",
			},
			true,
		},
		{
			"MCP endpoint returns protocol version",
			http.MethodPost,
			"/mcp",
			map[string]string{
				proxy.HeaderSessionID: "test-session",
			},
			true,
		},
	}

	for _, test := range healthTests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(test.method, "http://"+actualAddr+test.endpoint, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			for k, v := range test.headers {
				req.Header.Set(k, v)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if test.expectOK && resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			} else if !test.expectOK && resp.StatusCode == http.StatusOK {
				t.Errorf("expected non-200 status, got %d", resp.StatusCode)
			}

			if test.endpoint == "/healthz" {
				var result map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Errorf("failed to decode health response: %v", err)
				}
				if result["status"] != "ok" {
					t.Errorf("unexpected health status: %v", result)
				}
			}

			if test.endpoint == "/mcp" && test.expectOK {
				if resp.Header.Get(proxy.HeaderProtocolVersion) != proxy.MCPProtocolVersion {
					t.Errorf("missing protocol version header")
				}
				if resp.Header.Get(proxy.HeaderSessionID) != "test-session" {
					t.Errorf("missing session ID header")
				}
			}
		})
	}
}
