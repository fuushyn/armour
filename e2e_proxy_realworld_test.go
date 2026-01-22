//go:build e2e
// +build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
	"github.com/user/mcp-go-proxy/server"
)

// startHTTPProxy spins up the HTTP proxy against a supplied registry and returns the base URL and a shutdown func.
func startHTTPProxy(t *testing.T, registry *proxy.ServerRegistry) (string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")
	if err := proxy.SaveServerRegistry(registry, configPath); err != nil {
		t.Fatalf("failed to write registry: %v", err)
	}

	cfg := server.Config{
		ListenAddr: ":0",
		Mode:       "http",
		ConfigPath: configPath,
		LogLevel:   "error",
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create proxy server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	baseURL := waitForProxyReady(t, srv)
	shutdown := func() {
		cancel()
		_ = srv.Close()
	}
	return baseURL, shutdown
}

// waitForProxyReady polls /healthz until the proxy is reachable.
func waitForProxyReady(t *testing.T, srv *server.Server) string {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}

	for i := 0; i < 50; i++ {
		addr := srv.GetListenAddr()
		if strings.Contains(addr, ":") {
			url := fmt.Sprintf("http://%s/healthz", addr)
			if resp, err := client.Get(url); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return "http://" + addr
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("proxy did not become ready in time")
	return ""
}

// doProxyRequest sends a JSON-RPC request through the HTTP proxy to a specific backend.
func doProxyRequest(t *testing.T, baseURL, serverName, sessionID string, request JSONRPCRequest) JSONRPCResponse {
	t.Helper()

	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, sessionID)
	req.Header.Set(proxy.HeaderServerID, serverName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return rpcResp
}

// TestE2E_HTTPProxyWithPopularServers verifies the proxy can route to a variety of representative MCP servers (GitHub, Slack, etc.).
func TestE2E_HTTPProxyWithPopularServers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Stand up a set of mock MCP servers that mimic popular ones by name/tool shape.
	type stubServer struct {
		name       string
		mock       *MockMCPServer
		toolNames  []string
		hasContent bool
	}

	stubs := []stubServer{
		{
			name: "github",
			toolNames: []string{
				"github.repos.list",
				"github.issues.search",
				"github.pull_requests.create",
			},
		},
		{
			name: "slack",
			toolNames: []string{
				"slack.chat.postMessage",
				"slack.channels.list",
			},
		},
		{
			name: "filesystem",
			toolNames: []string{
				"fs.read_file",
				"fs.write_file",
			},
			hasContent: true,
		},
		{
			name: "kubernetes",
			toolNames: []string{
				"kube.get",
				"kube.apply",
			},
		},
	}

	registry := &proxy.ServerRegistry{}
	for i := range stubs {
		tools := make([]map[string]interface{}, 0, len(stubs[i].toolNames))
		for _, tool := range stubs[i].toolNames {
			tools = append(tools, map[string]interface{}{
				"name":        tool,
				"description": fmt.Sprintf("Tool %s", tool),
				"inputSchema": map[string]interface{}{"type": "object"},
			})
		}

		stubs[i].mock = NewMockMCPServer(t, tools)
		if stubs[i].hasContent {
			stubs[i].mock.SetResources([]map[string]interface{}{
				{"uri": "file:///tmp/test.txt", "name": "sample", "mimeType": "text/plain"},
			})
		}

		registry.Servers = append(registry.Servers, proxy.ServerEntry{
			Name:      stubs[i].name,
			Transport: "http",
			URL:       stubs[i].mock.URL,
		})
	}
	defer func() {
		for _, stub := range stubs {
			stub.mock.Close()
		}
	}()

	baseURL, shutdown := startHTTPProxy(t, registry)
	defer shutdown()

	for _, stub := range stubs {
		sessionID := fmt.Sprintf("session-%s", stub.name)

		// Initialize the upstream server through the proxy.
		initReq := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "initialize",
			Params: json.RawMessage(fmt.Sprintf(`{
				"protocolVersion": "%s",
				"capabilities": {"tools":{"listChanged":true}},
				"clientInfo": {"name":"e2e","version":"1.0"}
			}`, proxy.MCPProtocolVersion)),
		}
		initResp := doProxyRequest(t, baseURL, stub.name, sessionID, initReq)
		if initResp.Error != nil {
			t.Fatalf("[%s] initialize returned error: %v", stub.name, initResp.Error.Message)
		}

		// Fetch tools from the server to ensure forwarding works.
		toolsResp := doProxyRequest(t, baseURL, stub.name, sessionID, JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/list",
		})
		if toolsResp.Error != nil {
			t.Fatalf("[%s] tools/list returned error: %v", stub.name, toolsResp.Error.Message)
		}

		var toolsResult struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.Unmarshal(toolsResp.Result, &toolsResult); err != nil {
			t.Fatalf("[%s] failed to parse tools result: %v", stub.name, err)
		}

		if len(toolsResult.Tools) != len(stub.toolNames) {
			t.Fatalf("[%s] expected %d tools, got %d", stub.name, len(stub.toolNames), len(toolsResult.Tools))
		}

		// If the server exposes resources, verify resources/list flows through the proxy.
		if stub.hasContent {
			resourcesResp := doProxyRequest(t, baseURL, stub.name, sessionID, JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      3,
				Method:  "resources/list",
			})
			if resourcesResp.Error != nil {
				t.Fatalf("[%s] resources/list returned error: %v", stub.name, resourcesResp.Error.Message)
			}
		}
	}
}
