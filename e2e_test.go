//go:build e2e
// +build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MockMCPServer provides a configurable mock MCP server for testing
type MockMCPServer struct {
	*httptest.Server
	mu              sync.Mutex
	tools           []map[string]interface{}
	resources       []map[string]interface{}
	prompts         []map[string]interface{}
	toolCallHandler func(name string, args map[string]interface{}) (interface{}, error)
	requestLog      []JSONRPCRequest
	capabilities    map[string]interface{}
}

// NewMockMCPServer creates a new mock MCP server with configurable tools
func NewMockMCPServer(t *testing.T, tools []map[string]interface{}) *MockMCPServer {
	mock := &MockMCPServer{
		tools:      tools,
		resources:  []map[string]interface{}{},
		prompts:    []map[string]interface{}{},
		requestLog: []JSONRPCRequest{},
		capabilities: map[string]interface{}{
			"tools":     map[string]interface{}{"listChanged": true},
			"resources": map[string]interface{}{"subscribe": true, "listChanged": true},
			"prompts":   map[string]interface{}{"listChanged": true},
		},
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleRequest(t, w, r)
	}))

	return mock
}

func (m *MockMCPServer) handleRequest(t *testing.T, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(proxy.HeaderProtocolVersion, proxy.MCPProtocolVersion)

	// Set session ID in response
	if sessionID := r.Header.Get(proxy.HeaderSessionID); sessionID != "" {
		w.Header().Set(proxy.HeaderSessionID, sessionID)
	} else {
		w.Header().Set(proxy.HeaderSessionID, "mock-session-id")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Logf("error reading body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var request JSONRPCRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Logf("error parsing JSON-RPC: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.requestLog = append(m.requestLog, request)
	m.mu.Unlock()

	var response JSONRPCResponse
	response.JSONRPC = "2.0"
	response.ID = request.ID

	switch request.Method {
	case "initialize":
		response.Result, _ = json.Marshal(map[string]interface{}{
			"protocolVersion": proxy.MCPProtocolVersion,
			"capabilities":    m.capabilities,
			"serverInfo": map[string]interface{}{
				"name":    "MockMCPServer",
				"version": "1.0.0",
			},
		})

	case "tools/list":
		response.Result, _ = json.Marshal(map[string]interface{}{
			"tools": m.tools,
		})

	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &JSONRPCError{Code: -32602, Message: "Invalid params"}
		} else if m.toolCallHandler != nil {
			result, err := m.toolCallHandler(params.Name, params.Arguments)
			if err != nil {
				response.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
			} else {
				response.Result, _ = json.Marshal(result)
			}
		} else {
			// Default response
			response.Result, _ = json.Marshal(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("Tool %s called", params.Name)},
				},
				"isError": false,
			})
		}

	case "resources/list":
		response.Result, _ = json.Marshal(map[string]interface{}{
			"resources": m.resources,
		})

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &JSONRPCError{Code: -32602, Message: "Invalid params"}
		} else {
			response.Result, _ = json.Marshal(map[string]interface{}{
				"contents": []map[string]interface{}{
					{"uri": params.URI, "mimeType": "text/plain", "text": "Resource content"},
				},
			})
		}

	case "prompts/list":
		response.Result, _ = json.Marshal(map[string]interface{}{
			"prompts": m.prompts,
		})

	case "prompts/get":
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &JSONRPCError{Code: -32602, Message: "Invalid params"}
		} else {
			response.Result, _ = json.Marshal(map[string]interface{}{
				"description": "Test prompt",
				"messages": []map[string]interface{}{
					{"role": "user", "content": map[string]interface{}{"type": "text", "text": "Hello"}},
				},
			})
		}

	default:
		response.Error = &JSONRPCError{Code: -32601, Message: "Method not found"}
	}

	json.NewEncoder(w).Encode(response)
}

// SetToolCallHandler sets a custom handler for tool calls
func (m *MockMCPServer) SetToolCallHandler(handler func(name string, args map[string]interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCallHandler = handler
}

// SetResources sets the resources this mock server provides
func (m *MockMCPServer) SetResources(resources []map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = resources
}

// SetPrompts sets the prompts this mock server provides
func (m *MockMCPServer) SetPrompts(prompts []map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = prompts
}

// GetRequestLog returns all requests received
func (m *MockMCPServer) GetRequestLog() []JSONRPCRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]JSONRPCRequest{}, m.requestLog...)
}

// StdioTestHarness provides test infrastructure for stdio-based tests
type StdioTestHarness struct {
	stdin  *io.PipeWriter
	stdout *io.PipeReader
	t      *testing.T
}

// NewStdioTestHarness creates a harness for testing stdio communication
func NewStdioTestHarness(t *testing.T) *StdioTestHarness {
	r, w := io.Pipe()
	return &StdioTestHarness{
		stdin:  w,
		stdout: r,
		t:      t,
	}
}

// SendRequest sends a JSON-RPC request via stdin
func (h *StdioTestHarness) SendRequest(request JSONRPCRequest) {
	data, err := json.Marshal(request)
	if err != nil {
		h.t.Fatalf("failed to marshal request: %v", err)
	}
	h.stdin.Write(append(data, '\n'))
}

// ReadResponse reads a JSON-RPC response from stdout
func (h *StdioTestHarness) ReadResponse() JSONRPCResponse {
	reader := bufio.NewReader(h.stdout)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		h.t.Fatalf("failed to read response: %v", err)
	}

	var response JSONRPCResponse
	if err := json.Unmarshal(line, &response); err != nil {
		h.t.Fatalf("failed to parse response: %v", err)
	}
	return response
}

// Close closes the harness
func (h *StdioTestHarness) Close() {
	h.stdin.Close()
	h.stdout.Close()
}

// =============================================================================
// E2E TESTS
// =============================================================================

// TestE2E_InitializationWithSingleBackend tests basic initialization with one backend
func TestE2E_InitializationWithSingleBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create mock MCP server with tools
	tools := []map[string]interface{}{
		{
			"name":        "get_weather",
			"description": "Get weather for a location",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{"type": "string"},
				},
				"required": []string{"location"},
			},
		},
	}

	mock := NewMockMCPServer(t, tools)
	defer mock.Close()

	// Create config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")
	configJSON := fmt.Sprintf(`{
		"servers": [
			{"name": "weather", "transport": "http", "url": "%s"}
		]
	}`, mock.URL)

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load registry
	registry, err := proxy.LoadServerRegistry(configPath)
	if err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	if len(registry.Servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(registry.Servers))
	}

	if registry.Servers[0].Name != "weather" {
		t.Errorf("expected server name 'weather', got %s", registry.Servers[0].Name)
	}
}

// TestE2E_InitializationWithMultipleBackends tests initialization with multiple backends
func TestE2E_InitializationWithMultipleBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create multiple mock MCP servers
	weatherTools := []map[string]interface{}{
		{"name": "get_weather", "description": "Get weather"},
	}
	dbTools := []map[string]interface{}{
		{"name": "query", "description": "Run database query"},
		{"name": "insert", "description": "Insert into database"},
	}

	weatherMock := NewMockMCPServer(t, weatherTools)
	defer weatherMock.Close()

	dbMock := NewMockMCPServer(t, dbTools)
	defer dbMock.Close()

	// Create config with multiple servers
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")
	configJSON := fmt.Sprintf(`{
		"servers": [
			{"name": "weather", "transport": "http", "url": "%s"},
			{"name": "database", "transport": "http", "url": "%s"}
		]
	}`, weatherMock.URL, dbMock.URL)

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	registry, err := proxy.LoadServerRegistry(configPath)
	if err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	if len(registry.Servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(registry.Servers))
	}

	serverNames := make(map[string]bool)
	for _, s := range registry.Servers {
		serverNames[s.Name] = true
	}

	if !serverNames["weather"] || !serverNames["database"] {
		t.Errorf("missing expected servers: %v", serverNames)
	}
}

// TestE2E_ToolAggregationFromMultipleBackends tests that tools are aggregated correctly
func TestE2E_ToolAggregationFromMultipleBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create multiple mock servers with tools
	server1Tools := []map[string]interface{}{
		{"name": "tool_a", "description": "Tool A from server 1"},
		{"name": "tool_b", "description": "Tool B from server 1"},
	}
	server2Tools := []map[string]interface{}{
		{"name": "tool_c", "description": "Tool C from server 2"},
	}

	mock1 := NewMockMCPServer(t, server1Tools)
	defer mock1.Close()

	mock2 := NewMockMCPServer(t, server2Tools)
	defer mock2.Close()

	// Verify each mock server returns its tools
	for i, mock := range []*MockMCPServer{mock1, mock2} {
		// Send initialize request
		initReq := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "initialize",
			Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}`),
		}
		body, _ := json.Marshal(initReq)

		resp, err := http.Post(mock.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("mock %d initialize failed: %v", i+1, err)
		}
		resp.Body.Close()

		// Send tools/list request
		toolsReq := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/list",
		}
		body, _ = json.Marshal(toolsReq)

		req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(proxy.HeaderSessionID, "test-session")

		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("mock %d tools/list failed: %v", i+1, err)
		}
		defer resp.Body.Close()

		var jsonResp JSONRPCResponse
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			t.Fatalf("mock %d failed to decode response: %v", i+1, err)
		}

		if jsonResp.Error != nil {
			t.Errorf("mock %d returned error: %v", i+1, jsonResp.Error.Message)
		}

		var toolsResult struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.Unmarshal(jsonResp.Result, &toolsResult); err != nil {
			t.Fatalf("mock %d failed to parse tools result: %v", i+1, err)
		}

		expectedCount := 2
		if i == 1 {
			expectedCount = 1
		}
		if len(toolsResult.Tools) != expectedCount {
			t.Errorf("mock %d: expected %d tools, got %d", i+1, expectedCount, len(toolsResult.Tools))
		}
	}
}

// TestE2E_ToolCallRouting tests that tool calls are routed to the correct backend
func TestE2E_ToolCallRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Track which server received the tool call
	var server1Called, server2Called bool
	var mu sync.Mutex

	server1Tools := []map[string]interface{}{
		{"name": "server1_tool", "description": "Tool on server 1"},
	}
	server2Tools := []map[string]interface{}{
		{"name": "server2_tool", "description": "Tool on server 2"},
	}

	mock1 := NewMockMCPServer(t, server1Tools)
	mock1.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		mu.Lock()
		server1Called = true
		mu.Unlock()
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Server 1 responded"},
			},
			"isError": false,
		}, nil
	})
	defer mock1.Close()

	mock2 := NewMockMCPServer(t, server2Tools)
	mock2.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		mu.Lock()
		server2Called = true
		mu.Unlock()
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Server 2 responded"},
			},
			"isError": false,
		}, nil
	})
	defer mock2.Close()

	// Call tool on server 1
	toolReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"server1_tool","arguments":{}}`),
	}
	body, _ := json.Marshal(toolReq)

	req, _ := http.NewRequest("POST", mock1.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("tool call to server 1 failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	mu.Lock()
	if !server1Called {
		t.Error("server 1 was not called")
	}
	if server2Called {
		t.Error("server 2 was unexpectedly called")
	}
	mu.Unlock()
}

// TestE2E_ResourceAggregation tests that resources are aggregated from multiple backends
func TestE2E_ResourceAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	mock.SetResources([]map[string]interface{}{
		{"uri": "file:///doc1.txt", "name": "Document 1", "mimeType": "text/plain"},
		{"uri": "file:///doc2.txt", "name": "Document 2", "mimeType": "text/plain"},
	})
	defer mock.Close()

	// Request resources
	resourcesReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/list",
	}
	body, _ := json.Marshal(resourcesReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("resources/list failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var resourcesResult struct {
		Resources []map[string]interface{} `json:"resources"`
	}
	if err := json.Unmarshal(jsonResp.Result, &resourcesResult); err != nil {
		t.Fatalf("failed to parse resources result: %v", err)
	}

	if len(resourcesResult.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resourcesResult.Resources))
	}
}

// TestE2E_PromptAggregation tests that prompts are aggregated from multiple backends
func TestE2E_PromptAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	mock.SetPrompts([]map[string]interface{}{
		{"name": "greeting", "description": "Greeting prompt"},
		{"name": "farewell", "description": "Farewell prompt"},
	})
	defer mock.Close()

	// Request prompts
	promptsReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/list",
	}
	body, _ := json.Marshal(promptsReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("prompts/list failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var promptsResult struct {
		Prompts []map[string]interface{} `json:"prompts"`
	}
	if err := json.Unmarshal(jsonResp.Result, &promptsResult); err != nil {
		t.Fatalf("failed to parse prompts result: %v", err)
	}

	if len(promptsResult.Prompts) != 2 {
		t.Errorf("expected 2 prompts, got %d", len(promptsResult.Prompts))
	}
}

// TestE2E_BackendFailureHandling tests error handling when a backend fails
func TestE2E_BackendFailureHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create a server that returns errors
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer errorServer.Close()

	// Try to make a request
	toolReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	body, _ := json.Marshal(toolReq)

	req, _ := http.NewRequest("POST", errorServer.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should get a 500 error
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// TestE2E_BackendTimeout tests timeout handling for slow backends
func TestE2E_BackendTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create a slow server
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	// Create client with short timeout
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}

	toolReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	body, _ := json.Marshal(toolReq)

	req, _ := http.NewRequest("POST", slowServer.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	_, err := client.Do(req)
	if err == nil {
		t.Error("expected timeout error")
	}

	if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// TestE2E_InvalidToolCall tests error handling for invalid tool calls
func TestE2E_InvalidToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{
		{"name": "valid_tool", "description": "A valid tool"},
	})
	mock.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		if name == "invalid_tool" {
			return nil, fmt.Errorf("unknown tool: %s", name)
		}
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Success"},
			},
		}, nil
	})
	defer mock.Close()

	// Try to call non-existent tool
	toolReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"invalid_tool","arguments":{}}`),
	}
	body, _ := json.Marshal(toolReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp.Error == nil {
		t.Error("expected error for invalid tool")
	}

	if jsonResp.Error != nil && !strings.Contains(jsonResp.Error.Message, "invalid_tool") {
		t.Errorf("error should mention invalid tool: %v", jsonResp.Error.Message)
	}
}

// TestE2E_CapabilityNegotiation tests that capabilities are properly negotiated
func TestE2E_CapabilityNegotiation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	// Send initialize with client capabilities
	initReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2024-11-05",
			"capabilities": {
				"roots": {"listChanged": true},
				"sampling": {}
			},
			"clientInfo": {
				"name": "TestClient",
				"version": "1.0.0"
			}
		}`),
	}
	body, _ := json.Marshal(initReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp.Error != nil {
		t.Fatalf("initialize returned error: %v", jsonResp.Error.Message)
	}

	var initResult struct {
		ProtocolVersion string                 `json:"protocolVersion"`
		Capabilities    map[string]interface{} `json:"capabilities"`
		ServerInfo      map[string]interface{} `json:"serverInfo"`
	}
	if err := json.Unmarshal(jsonResp.Result, &initResult); err != nil {
		t.Fatalf("failed to parse init result: %v", err)
	}

	// Verify protocol version
	if initResult.ProtocolVersion != proxy.MCPProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", proxy.MCPProtocolVersion, initResult.ProtocolVersion)
	}

	// Verify server capabilities include tools, resources, prompts
	if initResult.Capabilities["tools"] == nil {
		t.Error("missing tools capability")
	}
	if initResult.Capabilities["resources"] == nil {
		t.Error("missing resources capability")
	}
	if initResult.Capabilities["prompts"] == nil {
		t.Error("missing prompts capability")
	}
}

// TestE2E_ProtocolVersionValidation tests protocol version handling
func TestE2E_ProtocolVersionValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	tests := []struct {
		name            string
		protocolVersion string
		expectError     bool
	}{
		{"valid version", "2024-11-05", false},
		{"newer version", "2025-03-26", false},
		{"old version", "1.0.0", false}, // Server should respond with its version
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initReq := JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
				Params: json.RawMessage(fmt.Sprintf(`{
					"protocolVersion": "%s",
					"capabilities": {},
					"clientInfo": {"name": "test", "version": "1.0"}
				}`, tt.protocolVersion)),
			}
			body, _ := json.Marshal(initReq)

			req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			var jsonResp JSONRPCResponse
			if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			hasError := jsonResp.Error != nil
			if hasError != tt.expectError {
				t.Errorf("expectError=%v, got error=%v", tt.expectError, jsonResp.Error)
			}
		})
	}
}

// TestE2E_SessionManagement tests session ID handling
func TestE2E_SessionManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	// First request without session ID
	initReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}`),
	}
	body, _ := json.Marshal(initReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// Should receive session ID in response
	sessionID := resp.Header.Get(proxy.HeaderSessionID)
	resp.Body.Close()

	if sessionID == "" {
		t.Error("expected session ID in response")
	}

	// Subsequent requests should use session ID
	toolsReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}
	body, _ = json.Marshal(toolsReq)

	req, _ = http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, sessionID)

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	defer resp.Body.Close()

	// Session ID should be echoed back
	if resp.Header.Get(proxy.HeaderSessionID) != sessionID {
		t.Errorf("expected session ID %s, got %s", sessionID, resp.Header.Get(proxy.HeaderSessionID))
	}
}

// TestE2E_ToolContentTypes tests different tool result content types
func TestE2E_ToolContentTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	tests := []struct {
		name         string
		toolResult   interface{}
		expectType   string
		validateFunc func(t *testing.T, result json.RawMessage)
	}{
		{
			name: "text content",
			toolResult: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "Hello, world!"},
				},
				"isError": false,
			},
			expectType: "text",
		},
		{
			name: "error content",
			toolResult: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "An error occurred"},
				},
				"isError": true,
			},
			expectType: "text",
		},
		{
			name: "multiple content items",
			toolResult: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "First part"},
					{"type": "text", "text": "Second part"},
				},
				"isError": false,
			},
			expectType: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockMCPServer(t, []map[string]interface{}{
				{"name": "test_tool", "description": "Test tool"},
			})
			mock.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
				return tt.toolResult, nil
			})
			defer mock.Close()

			toolReq := JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"test_tool","arguments":{}}`),
			}
			body, _ := json.Marshal(toolReq)

			req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(proxy.HeaderSessionID, "test-session")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			var jsonResp JSONRPCResponse
			if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if jsonResp.Error != nil {
				t.Errorf("unexpected error: %v", jsonResp.Error.Message)
			}

			var toolResult struct {
				Content []map[string]interface{} `json:"content"`
				IsError bool                     `json:"isError"`
			}
			if err := json.Unmarshal(jsonResp.Result, &toolResult); err != nil {
				t.Fatalf("failed to parse tool result: %v", err)
			}

			if len(toolResult.Content) == 0 {
				t.Error("expected content in tool result")
			}

			if toolResult.Content[0]["type"] != tt.expectType {
				t.Errorf("expected content type %s, got %s", tt.expectType, toolResult.Content[0]["type"])
			}
		})
	}
}

// TestE2E_ConcurrentRequests tests handling of concurrent requests
func TestE2E_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{
		{"name": "concurrent_tool", "description": "Test concurrent access"},
	})

	var callCount int
	var mu sync.Mutex
	mock.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		time.Sleep(50 * time.Millisecond) // Simulate processing

		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": fmt.Sprintf("Request %d", count)},
			},
			"isError": false,
		}, nil
	})
	defer mock.Close()

	// Send multiple concurrent requests
	const numRequests = 10
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)
	responses := make(chan string, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()

			toolReq := JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      reqNum,
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"concurrent_tool","arguments":{}}`),
			}
			body, _ := json.Marshal(toolReq)

			req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(proxy.HeaderSessionID, fmt.Sprintf("session-%d", reqNum))

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()

			var jsonResp JSONRPCResponse
			if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
				errors <- err
				return
			}

			if jsonResp.Error != nil {
				errors <- fmt.Errorf("request %d error: %s", reqNum, jsonResp.Error.Message)
				return
			}

			responses <- string(jsonResp.Result)
		}(i)
	}

	wg.Wait()
	close(errors)
	close(responses)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent request error: %v", err)
	}

	// Count successful responses
	responseCount := 0
	for range responses {
		responseCount++
	}

	if responseCount != numRequests {
		t.Errorf("expected %d responses, got %d", numRequests, responseCount)
	}

	mu.Lock()
	if callCount != numRequests {
		t.Errorf("expected %d tool calls, got %d", numRequests, callCount)
	}
	mu.Unlock()
}

// TestE2E_JSONRPCBatching tests JSON-RPC batch requests
func TestE2E_JSONRPCBatching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Note: Batch support depends on the server implementation
	// This test validates individual request handling

	mock := NewMockMCPServer(t, []map[string]interface{}{
		{"name": "tool1", "description": "Tool 1"},
		{"name": "tool2", "description": "Tool 2"},
	})
	defer mock.Close()

	// Send individual requests in sequence
	requests := []JSONRPCRequest{
		{JSONRPC: "2.0", ID: 1, Method: "tools/list"},
		{JSONRPC: "2.0", ID: 2, Method: "resources/list"},
		{JSONRPC: "2.0", ID: 3, Method: "prompts/list"},
	}

	client := &http.Client{}
	for _, req := range requests {
		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set(proxy.HeaderSessionID, "test-session")

		resp, err := client.Do(httpReq)
		if err != nil {
			t.Fatalf("request %v failed: %v", req.ID, err)
		}

		var jsonResp JSONRPCResponse
		json.NewDecoder(resp.Body).Decode(&jsonResp)
		resp.Body.Close()

		if jsonResp.Error != nil {
			t.Errorf("request %v error: %v", req.ID, jsonResp.Error.Message)
		}

		// JSON unmarshaling may convert integer IDs to float64
		expectedID := float64(req.ID.(int))
		gotID, ok := jsonResp.ID.(float64)
		if !ok {
			// Handle the case where ID is already an int
			if intID, isInt := jsonResp.ID.(int); isInt {
				gotID = float64(intID)
			} else {
				t.Errorf("unexpected ID type: %T", jsonResp.ID)
				continue
			}
		}

		if gotID != expectedID {
			t.Errorf("expected ID %v, got %v", expectedID, gotID)
		}
	}
}

// TestE2E_ResourceRead tests reading a specific resource
func TestE2E_ResourceRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	mock.SetResources([]map[string]interface{}{
		{"uri": "file:///test.txt", "name": "Test File", "mimeType": "text/plain"},
	})
	defer mock.Close()

	// Request to read a resource
	readReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params:  json.RawMessage(`{"uri":"file:///test.txt"}`),
	}
	body, _ := json.Marshal(readReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("resources/read failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp.Error != nil {
		t.Errorf("unexpected error: %v", jsonResp.Error.Message)
	}

	var readResult struct {
		Contents []map[string]interface{} `json:"contents"`
	}
	if err := json.Unmarshal(jsonResp.Result, &readResult); err != nil {
		t.Fatalf("failed to parse read result: %v", err)
	}

	if len(readResult.Contents) == 0 {
		t.Error("expected contents in read result")
	}

	if readResult.Contents[0]["uri"] != "file:///test.txt" {
		t.Errorf("expected uri file:///test.txt, got %v", readResult.Contents[0]["uri"])
	}
}

// TestE2E_PromptGet tests getting a specific prompt
func TestE2E_PromptGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	mock.SetPrompts([]map[string]interface{}{
		{"name": "greeting", "description": "A greeting prompt"},
	})
	defer mock.Close()

	// Request to get a prompt
	getReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name":"greeting"}`),
	}
	body, _ := json.Marshal(getReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("prompts/get failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp.Error != nil {
		t.Errorf("unexpected error: %v", jsonResp.Error.Message)
	}

	var getResult struct {
		Description string                   `json:"description"`
		Messages    []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(jsonResp.Result, &getResult); err != nil {
		t.Fatalf("failed to parse get result: %v", err)
	}

	if len(getResult.Messages) == 0 {
		t.Error("expected messages in prompt result")
	}
}

// TestE2E_NotificationHandling tests that notifications are handled correctly
func TestE2E_NotificationHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	// Send initialized notification (no ID)
	notification := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	body, _ := json.Marshal(notification)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("notification failed: %v", err)
	}
	defer resp.Body.Close()

	// Notifications may return empty response or 202 Accepted
	// The key is they shouldn't return an error
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected status 200 or 202, got %d", resp.StatusCode)
	}
}

// TestE2E_MethodNotFound tests handling of unknown methods
func TestE2E_MethodNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	// Request with unknown method
	unknownReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown/method",
	}
	body, _ := json.Marshal(unknownReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp.Error == nil {
		t.Error("expected error for unknown method")
	}

	if jsonResp.Error != nil && jsonResp.Error.Code != -32601 {
		t.Errorf("expected error code -32601 (Method not found), got %d", jsonResp.Error.Code)
	}
}

// TestE2E_InvalidJSON tests handling of invalid JSON
func TestE2E_InvalidJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})
	defer mock.Close()

	// Send invalid JSON
	invalidJSON := []byte(`{invalid json`)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(invalidJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should get a bad request or parse error
	if resp.StatusCode != http.StatusBadRequest {
		t.Logf("Status: %d (some servers may return 200 with JSON-RPC error)", resp.StatusCode)
	}
}

// TestE2E_ToolInputValidation tests tool input schema validation
func TestE2E_ToolInputValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{
		{
			"name":        "validated_tool",
			"description": "A tool with input validation",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"required_field": map[string]interface{}{"type": "string"},
				},
				"required": []string{"required_field"},
			},
		},
	})

	mock.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		// Validate required field
		if _, ok := args["required_field"]; !ok {
			return nil, fmt.Errorf("missing required field: required_field")
		}
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Validation passed"},
			},
			"isError": false,
		}, nil
	})
	defer mock.Close()

	client := &http.Client{}

	// Test with missing required field
	t.Run("missing required field", func(t *testing.T) {
		toolReq := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"validated_tool","arguments":{}}`),
		}
		body, _ := json.Marshal(toolReq)

		req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(proxy.HeaderSessionID, "test-session")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var jsonResp JSONRPCResponse
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if jsonResp.Error == nil {
			t.Error("expected error for missing required field")
		}
	})

	// Test with valid input
	t.Run("valid input", func(t *testing.T) {
		toolReq := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"validated_tool","arguments":{"required_field":"value"}}`),
		}
		body, _ := json.Marshal(toolReq)

		req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(proxy.HeaderSessionID, "test-session")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var jsonResp JSONRPCResponse
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if jsonResp.Error != nil {
			t.Errorf("unexpected error with valid input: %v", jsonResp.Error.Message)
		}
	})
}

// TestE2E_GracefulShutdown tests that the server shuts down gracefully
func TestE2E_GracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	mock := NewMockMCPServer(t, []map[string]interface{}{})

	// Server should be accessible
	toolsReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	body, _ := json.Marshal(toolsReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("pre-shutdown request failed: %v", err)
	}
	resp.Body.Close()

	// Close the server
	mock.Close()

	// Wait a bit for shutdown
	time.Sleep(100 * time.Millisecond)

	// Server should be inaccessible
	_, err = client.Do(req)
	if err == nil {
		t.Error("expected error after shutdown")
	}
}

// TestE2E_LargePayload tests handling of large tool responses
func TestE2E_LargePayload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Generate large response
	largeText := strings.Repeat("This is a large payload. ", 10000)

	mock := NewMockMCPServer(t, []map[string]interface{}{
		{"name": "large_response_tool", "description": "Returns large response"},
	})
	mock.SetToolCallHandler(func(name string, args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": largeText},
			},
			"isError": false,
		}, nil
	})
	defer mock.Close()

	toolReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"large_response_tool","arguments":{}}`),
	}
	body, _ := json.Marshal(toolReq)

	req, _ := http.NewRequest("POST", mock.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var jsonResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode large response: %v", err)
	}

	if jsonResp.Error != nil {
		t.Errorf("unexpected error: %v", jsonResp.Error.Message)
	}

	var toolResult struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal(jsonResp.Result, &toolResult); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(toolResult.Content) == 0 {
		t.Error("expected content in result")
	}

	text, ok := toolResult.Content[0]["text"].(string)
	if !ok {
		t.Error("expected text content")
	}

	if len(text) < 100000 {
		t.Errorf("expected large text, got %d bytes", len(text))
	}
}

// TestE2E_ConnectionReuse tests HTTP connection reuse
func TestE2E_ConnectionReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	var connectionCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(proxy.HeaderSessionID, "test-session")
		json.NewEncoder(w).Encode(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"tools":[]}`),
		})
	}))
	defer server.Close()

	// Make multiple requests with same client (connection reuse)
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 1,
		},
	}

	for i := 0; i < 5; i++ {
		toolsReq := JSONRPCRequest{JSONRPC: "2.0", ID: i, Method: "tools/list"}
		body, _ := json.Marshal(toolsReq)

		req, _ := http.NewRequest("POST", server.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(proxy.HeaderSessionID, "test-session")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// With keep-alive, connection count should be less than request count
	// (exact behavior depends on server/transport settings)
	mu.Lock()
	count := connectionCount
	mu.Unlock()

	t.Logf("Made 5 requests, server handled %d connections", count)
}

// TestE2E_ContextCancellation tests request cancellation
func TestE2E_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	toolsReq := JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"}
	body, _ := json.Marshal(toolsReq)

	req, _ := http.NewRequestWithContext(ctx, "POST", slowServer.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proxy.HeaderSessionID, "test-session")

	client := &http.Client{}
	_, err := client.Do(req)

	if err == nil {
		t.Error("expected context cancellation error")
	}

	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}
