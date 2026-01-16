package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
)

// autoDiscoverServersFromClaudeConfig reads Claude Code's config and extracts MCP servers
func (bm *BackendManager) autoDiscoverServersFromClaudeConfig() []proxy.ServerEntry {
	defer func() {
		if r := recover(); r != nil {
			bm.logger.Error("panic during auto-discovery: %v", r)
		}
	}()

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		return nil
	}

	configPath := path.Join(homeDir, ".claude.json")
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		bm.logger.Debug("failed to read Claude Code config: %v", err)
		return nil
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		bm.logger.Debug("failed to parse Claude Code config: %v", err)
		return nil
	}

	servers := []proxy.ServerEntry{}
	seenServers := make(map[string]bool)

	// Recursively find all mcpServers entries
	bm.extractMCPServers(config, &servers, seenServers)

	return servers
}

// extractMCPServers recursively finds all mcpServers in the config
func (bm *BackendManager) extractMCPServers(data interface{}, servers *[]proxy.ServerEntry, seen map[string]bool) {
	switch v := data.(type) {
	case map[string]interface{}:
		// Check if this object has mcpServers
		if mcpServers, ok := v["mcpServers"].(map[string]interface{}); ok {
			for serverName, serverConfig := range mcpServers {
				// Skip if we've already seen this server (avoid duplicates)
				if seen[serverName] {
					continue
				}
				seen[serverName] = true

				// Skip the proxy itself
				if serverName == "mcp-proxy" || serverName == "plugin:.claude-plugin:mcp-go-proxy" {
					continue
				}

				// Convert Claude Code server config to proxy ServerEntry
				if serverConfigMap, ok := serverConfig.(map[string]interface{}); ok {
					entry := bm.convertClaudeConfigToServerEntry(serverName, serverConfigMap)
					if entry != nil {
						*servers = append(*servers, *entry)
					}
				}
			}
		}

		// Recursively check all nested objects
		for _, val := range v {
			bm.extractMCPServers(val, servers, seen)
		}

	case []interface{}:
		// Recursively check all items in arrays
		for _, item := range v {
			bm.extractMCPServers(item, servers, seen)
		}
	}
}

// convertClaudeConfigToServerEntry converts a Claude Code MCP server config to proxy ServerEntry
func (bm *BackendManager) convertClaudeConfigToServerEntry(name string, config map[string]interface{}) *proxy.ServerEntry {
	entry := &proxy.ServerEntry{
		Name: name,
	}

	// Get transport type
	if typeVal, ok := config["type"].(string); ok {
		entry.Transport = typeVal
	} else {
		// Default based on other fields
		if _, hasURL := config["url"]; hasURL {
			entry.Transport = "http"
		} else if _, hasCommand := config["command"]; hasCommand {
			entry.Transport = "stdio"
		} else {
			return nil // Can't determine transport
		}
	}

	// Get URL for HTTP servers
	if url, ok := config["url"].(string); ok {
		entry.URL = url
	}

	// Get command for stdio servers
	if cmd, ok := config["command"].(string); ok {
		entry.Command = cmd
	}

	return entry
}

// BackendConnection represents a connection to a single backend MCP server.
type BackendConnection struct {
	config       *proxy.ServerEntry
	transport    proxy.Transport
	initialized  bool
	Capabilities *proxy.Capabilities
	tools        []Tool
	mu           sync.RWMutex
	logger       *proxy.Logger
}

// Tool represents an MCP tool with its metadata.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// BackendManager manages connections to multiple backend MCP servers.
type BackendManager struct {
	registry            *proxy.ServerRegistry
	logger              *proxy.Logger
	connections         map[string]*BackendConnection
	mu                  sync.RWMutex
	toolRegistry        *ToolRegistry
	initializationDone  chan struct{}
	initializationOnce  sync.Once
}

// NewBackendManager creates a new backend manager.
func NewBackendManager(registry *proxy.ServerRegistry, logger *proxy.Logger, toolRegistry *ToolRegistry) *BackendManager {
	return &BackendManager{
		registry:           registry,
		logger:             logger,
		connections:        make(map[string]*BackendConnection),
		toolRegistry:       toolRegistry,
		initializationDone: make(chan struct{}),
	}
}

// Initialize attempts to initialize all configured backend servers.
// If no servers are configured, auto-discovers from Claude Code's config.
// Partial failures are logged but don't prevent initialization of other backends.
func (bm *BackendManager) Initialize(ctx context.Context) error {
	defer bm.initializationOnce.Do(func() {
		close(bm.initializationDone)
	})

	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.registry == nil || len(bm.registry.Servers) == 0 {
		bm.logger.Debug("no backend servers configured in registry, operating with empty backend list")
		bm.logger.Debug("use proxy:detect-servers and proxy:migrate-config tools to add servers")
		return nil
	}

	var initErrors []error

	for i := range bm.registry.Servers {
		if err := bm.initializeBackend(ctx, &bm.registry.Servers[i]); err != nil {
			bm.logger.Error("failed to initialize backend %s: %v", bm.registry.Servers[i].Name, err)
			initErrors = append(initErrors, err)
		}
	}

	// If we initialized at least one backend, consider it a success
	if len(bm.connections) > 0 {
		bm.logger.Info("initialized %d backend servers", len(bm.connections))
		return nil
	}

	if len(initErrors) > 0 {
		return fmt.Errorf("failed to initialize any backends: %v", initErrors[0])
	}

	return nil
}

// WaitForInitialization waits for backend initialization to complete with a timeout.
func (bm *BackendManager) WaitForInitialization(ctx context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-bm.initializationDone:
		bm.logger.Debug("backend initialization complete")
	case <-ctx.Done():
		bm.logger.Warn("backend initialization timeout")
	}
}

// initializeBackend initializes a single backend server.
func (bm *BackendManager) initializeBackend(ctx context.Context, serverEntry *proxy.ServerEntry) error {
	bm.logger.Debug("initializing backend: %s (%s)", serverEntry.Name, serverEntry.Transport)

	// Create transport based on server configuration
	var transport proxy.Transport

	switch serverEntry.Transport {
	case "stdio":
		// Spawn subprocess for stdio server
		cmd := exec.CommandContext(ctx, serverEntry.Command, serverEntry.Args...)

		// Set environment variables
		cmd.Env = append([]string{}, os.Environ()...)
		for k, v := range serverEntry.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}

		// Get pipes for communication
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdin pipe: %v", err)
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %v", err)
		}

		// Start the process
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start subprocess: %v", err)
		}

		// Create stdio transport
		transport = proxy.NewStdioTransport(stdout, stdin)

		// Store the command so we can clean it up later
		bm.logger.Debug("started stdio subprocess for %s", serverEntry.Name)

	case "http":
		// Create HTTP transport for this server
		// Generate session ID for the request; server will confirm in response header
		sessionID := generateSessionID()
		httpTransport := proxy.NewHTTPTransport(serverEntry.URL)
		// Manually set the initial session ID so it's sent on all requests including initialize
		httpTransport.SetSessionID(sessionID)
		if serverEntry.Headers != nil {
			httpTransport.SetHeaders(serverEntry.Headers)
		}
		transport = httpTransport

	case "sse":
		// Create SSE transport for this server
		sseTransport := proxy.NewSSETransport(serverEntry.URL)
		if serverEntry.Headers != nil {
			sseTransport.SetHeaders(serverEntry.Headers)
		}
		// Note: Don't call Connect() yet - SSE connection is established after initialize
		transport = sseTransport

	default:
		return fmt.Errorf("unsupported transport: %s", serverEntry.Transport)
	}

	// Initialize connection
	conn := &BackendConnection{
		config:      serverEntry,
		transport:   transport,
		logger:      bm.logger,
		initialized: false,
	}

	// Send initialize request to backend
	if err := conn.initialize(ctx); err != nil {
		return fmt.Errorf("backend initialization failed: %v", err)
	}

	// Get tools from backend
	if err := conn.getTools(ctx); err != nil {
		bm.logger.Warn("failed to get tools from backend: %v", err)
		// Don't fail on tool retrieval - backend might not have tools
	}

	// Register tools in registry
	if err := bm.toolRegistry.RegisterBackendTools(serverEntry.Name, conn.tools); err != nil {
		bm.logger.Warn("failed to register tools from %s: %v", serverEntry.Name, err)
	}

	// Store connection
	bm.connections[serverEntry.Name] = conn

	return nil
}

// GetInitializedBackends returns all successfully initialized backend connections.
func (bm *BackendManager) GetInitializedBackends() []*BackendConnection {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	backends := make([]*BackendConnection, 0, len(bm.connections))
	for _, conn := range bm.connections {
		if conn.initialized {
			backends = append(backends, conn)
		}
	}
	return backends
}

// CallTool sends a tool call request to a backend server.
func (bm *BackendManager) CallTool(ctx context.Context, backendID string, toolName string, arguments json.RawMessage) (interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	return conn.callTool(ctx, toolName, arguments)
}

// BackendConnection methods

// initialize sends an initialize request to the backend server.
func (bc *BackendConnection) initialize(ctx context.Context) error {
	// Build initialize request
	initReq := proxy.NewInitRequest("mcp-go-proxy", "1.0.0")

	// Send request to backend and get response
	respBytes, err := bc.sendRequest(ctx, initReq)
	if err != nil {
		return fmt.Errorf("failed to send initialize request: %v", err)
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Parse response
	var initResp struct {
		Result struct {
			Capabilities proxy.Capabilities `json:"capabilities"`
			ServerInfo   struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
		Error *struct {
			Code    int         `json:"code"`
			Message string      `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &initResp); err != nil {
		return fmt.Errorf("failed to parse initialize response: %v", err)
	}

	if initResp.Error != nil {
		return fmt.Errorf("backend returned error: %s", initResp.Error.Message)
	}

	// Validate protocol version
	if err := proxy.ValidateProtocolVersion(initResp.Result.ProtocolVersion, proxy.MCPProtocolVersion); err != nil {
		bc.logger.Warn("protocol version mismatch: %v", err)
		// Continue anyway - some servers might not enforce this strictly
	}

	bc.Capabilities = &initResp.Result.Capabilities
	bc.initialized = true

	bc.logger.Debug("backend initialized: %s v%s", initResp.Result.ServerInfo.Name, initResp.Result.ServerInfo.Version)

	// For SSE transport, establish the event stream after initialization
	if sseTransport, ok := bc.transport.(*proxy.SSETransport); ok {
		bc.mu.Unlock() // Release lock before Connect which takes its own lock
		if err := sseTransport.Connect(); err != nil {
			bc.logger.Error("failed to connect SSE stream: %v", err)
			return fmt.Errorf("failed to connect SSE stream: %v", err)
		}
		bc.mu.Lock() // Re-acquire lock
	}

	return nil
}

// getTools retrieves the list of available tools from the backend.
func (bc *BackendConnection) getTools(ctx context.Context) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if !bc.initialized {
		return fmt.Errorf("backend not initialized")
	}

	// Build tools/list request
	toolsListReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	// Send request
	respBytes, err := bc.sendRequestLocked(ctx, toolsListReq)
	if err != nil {
		return fmt.Errorf("failed to get tools: %v", err)
	}

	// Parse response
	var toolsResp struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &toolsResp); err != nil {
		return fmt.Errorf("failed to parse tools/list response: %v", err)
	}

	if toolsResp.Error != nil {
		return fmt.Errorf("backend returned error: %s", toolsResp.Error.Message)
	}

	bc.tools = toolsResp.Result.Tools
	return nil
}

// callTool sends a tool/call request to the backend.
func (bc *BackendConnection) callTool(ctx context.Context, toolName string, arguments json.RawMessage) (interface{}, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if !bc.initialized {
		return nil, fmt.Errorf("backend not initialized")
	}

	// Build tool/call request
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}

	paramsData, _ := json.Marshal(params)

	toolCallReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  paramsData,
	}

	// Send request
	respBytes, err := bc.sendRequestLocked(ctx, toolCallReq)
	if err != nil {
		return nil, fmt.Errorf("tool call failed: %v", err)
	}

	// Parse response
	var toolResp struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Code    int         `json:"code"`
			Message string      `json:"message"`
			Data    interface{} `json:"data"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &toolResp); err != nil {
		return nil, fmt.Errorf("failed to parse tool call response: %v", err)
	}

	if toolResp.Error != nil {
		return nil, fmt.Errorf("tool call error: %s", toolResp.Error.Message)
	}

	return toolResp.Result, nil
}

// sendRequest sends a request to the backend. MUST be called WITHOUT the lock held.
func (bc *BackendConnection) sendRequest(ctx context.Context, req interface{}) ([]byte, error) {
	bc.mu.RLock()
	transport := bc.transport
	bc.mu.RUnlock()

	if transport == nil {
		return nil, fmt.Errorf("transport not initialized")
	}

	// Marshal request to JSON
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Add newline for JSON-RPC line protocol
	reqWithNewline := append(reqBytes, '\n')

	bc.logger.Debug("sending request to backend: %s", string(reqBytes))

	// Send request
	if err := transport.SendMessage(reqWithNewline); err != nil {
		bc.logger.Error("failed to send request: %v", err)
		return nil, fmt.Errorf("failed to send request: %v", err)
	}

	bc.logger.Debug("request sent, waiting for response")

	// Create a channel to handle the response with timeout
	type response struct {
		data []byte
		err  error
	}
	respCh := make(chan response, 1)

	go func() {
		respBytes, err := transport.ReceiveMessage()
		respCh <- response{respBytes, err}
	}()

	// Wait for response with timeout
	select {
	case resp := <-respCh:
		if resp.err != nil {
			bc.logger.Error("failed to receive response: %v", resp.err)
			return nil, fmt.Errorf("failed to receive response: %v", resp.err)
		}
		bc.logger.Debug("received response from backend: %s", string(resp.data))
		return resp.data, nil

	case <-ctx.Done():
		bc.logger.Error("request context cancelled before response")
		return nil, fmt.Errorf("request cancelled: %v", ctx.Err())
	}
}

// sendRequestLocked is like sendRequest but assumes the write lock is already held.
// It temporarily releases the lock to avoid deadlock when calling sendRequest.
func (bc *BackendConnection) sendRequestLocked(ctx context.Context, req interface{}) ([]byte, error) {
	bc.mu.Unlock()
	defer bc.mu.Lock()

	return bc.sendRequest(ctx, req)
}

// generateSessionID generates a random session ID for HTTP transport.
func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	sessionID := ""
	for _, byte := range b {
		sessionID += fmt.Sprintf("%02x", byte)
	}
	return sessionID
}
