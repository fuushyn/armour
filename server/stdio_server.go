package server

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/user/mcp-go-proxy/cmd"
	"github.com/user/mcp-go-proxy/proxy"
)

// StdioServer implements an MCP server that reads JSON-RPC requests from stdin
// and writes responses to stdout. It proxies requests to multiple backend servers,
// aggregating their capabilities and tools.
type StdioServer struct {
	config         Config
	db             *sql.DB
	proxyManager   *proxy.Proxy
	sessionMgr     *proxy.SessionManager
	resourceMgr    *proxy.ResourceManager
	oauth          *proxy.OAuth
	securityMgr    *proxy.SecurityManager
	auditLog       *proxy.AuditLog
	registry       *proxy.ServerRegistry
	logger         *proxy.Logger
	policyManager  *PolicyManager
	backendManager *BackendManager
	toolRegistry   *ToolRegistry
	statsTracker   *StatsTracker

	// Request/response handling
	scanner *bufio.Scanner
	encoder *json.Encoder
	mu      sync.RWMutex

	// Lifecycle
	initialized bool
	clientInfo  *proxy.ClientInfo
	serverCaps  *proxy.Capabilities
	clientCaps  *proxy.Capabilities
}

// NewStdioServer creates a new stdio-based MCP proxy server.
func NewStdioServer(config Config, registry *proxy.ServerRegistry, statsTracker *StatsTracker, policyManager *PolicyManager) (*StdioServer, error) {
	// Initialize database
	db, err := initializeDB(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %v", err)
	}

	logger := proxy.NewLogger(config.LogLevel)
	sessionMgr := proxy.NewSessionManager(db)
	resourceMgr := proxy.NewResourceManager()
	oauth := proxy.NewOAuth()
	securityMgr := proxy.NewSecurityManager()
	auditLog := proxy.NewAuditLog()

	// Add allowed origins
	for _, origin := range config.AllowedOrigins {
		securityMgr.AddAllowedOrigin(origin)
	}

	// Create tool registry (shared with backend manager)
	toolRegistry := NewToolRegistry()

	// Create backend manager (will use the shared tool registry)
	backendManager := NewBackendManager(registry, logger, toolRegistry)

	s := &StdioServer{
		config:         config,
		db:             db,
		proxyManager:   proxy.NewProxy(db),
		sessionMgr:     sessionMgr,
		resourceMgr:    resourceMgr,
		oauth:          oauth,
		securityMgr:    securityMgr,
		auditLog:       auditLog,
		registry:       registry,
		logger:         logger,
		policyManager:  policyManager,
		backendManager: backendManager,
		toolRegistry:   toolRegistry,
		statsTracker:   statsTracker,
		scanner:        bufio.NewScanner(os.Stdin),
		encoder:        json.NewEncoder(os.Stdout),
		initialized:    false,
	}

	return s, nil
}

// Run starts the stdio server, reading JSON-RPC requests from stdin and writing
// responses to stdout until EOF or error.
func (s *StdioServer) Run(ctx context.Context) error {
	s.logger.Info("stdio server started")
	defer s.logger.Info("stdio server stopped")

	// Configure scanner for large inputs
	s.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for s.scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse JSON-RPC request
		var request JSONRPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			s.logger.Error("failed to parse JSON-RPC request: %v", err)
			s.sendError(request.ID, -32700, "Parse error")
			continue
		}

		// Route to appropriate handler
		response := s.handleRequest(ctx, request)
		if err := s.encoder.Encode(response); err != nil {
			s.logger.Error("failed to encode response: %v", err)
			return err
		}
	}

	if err := s.scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// handleRequest routes a JSON-RPC request to the appropriate handler.
func (s *StdioServer) handleRequest(ctx context.Context, request JSONRPCRequest) interface{} {
	switch request.Method {
	case "initialize":
		return s.handleInitialize(ctx, request)
	case "notifications/initialized":
		return s.handleInitialized(ctx, request)
	case "tools/list":
		return s.handleToolsList(ctx, request)
	case "tools/call":
		return s.handleToolsCall(ctx, request)
	case "resources/list":
		return s.handleResourcesList(ctx, request)
	case "resources/read":
		return s.handleResourcesRead(ctx, request)
	case "prompts/list":
		return s.handlePromptsList(ctx, request)
	case "prompts/get":
		return s.handlePromptsGet(ctx, request)
	case "completion/complete":
		return s.handleCompletionComplete(ctx, request)
	default:
		s.logger.Warn("unknown method: %s", request.Method)
		return s.makeError(request.ID, -32601, "Method not found", request.Method)
	}
}

// handleInitialize initializes the proxy by connecting to all backends and
// aggregating their capabilities.
func (s *StdioServer) handleInitialize(ctx context.Context, request JSONRPCRequest) interface{} {
	var params struct {
		ClientInfo      proxy.ClientInfo   `json:"clientInfo"`
		Capabilities    proxy.Capabilities `json:"capabilities"`
		ProtocolVersion string             `json:"protocolVersion"`
	}

	if err := json.Unmarshal(request.Params, &params); err != nil {
		return s.makeError(request.ID, -32602, "Invalid params", err.Error())
	}

	// Validate protocol version
	if err := proxy.ValidateProtocolVersion(params.ProtocolVersion, proxy.MCPProtocolVersion); err != nil {
		return s.makeError(request.ID, -32602, "Protocol version mismatch", err.Error())
	}

	s.clientInfo = &params.ClientInfo
	s.clientCaps = &params.Capabilities

	// Initialize all backends (non-blocking - do in background)
	go func() {
		if err := s.backendManager.Initialize(ctx); err != nil {
			s.logger.Error("failed to initialize backends: %v", err)
		}
	}()

	// Aggregate capabilities from all backends
	s.serverCaps = s.aggregateCapabilities()

	// For compatibility, advertise server capabilities (do not AND with client caps)
	finalCaps := *s.serverCaps

	s.initialized = true

	// Build response
	result := map[string]interface{}{
		"serverInfo": map[string]string{
			"name":    "mcp-go-proxy",
			"version": "1.0.0",
		},
		"capabilities":    finalCaps,
		"protocolVersion": proxy.MCPProtocolVersion,
	}

	return s.makeResult(request.ID, result)
}

// handleInitialized handles the initialized notification.
func (s *StdioServer) handleInitialized(ctx context.Context, request JSONRPCRequest) interface{} {
	s.logger.Debug("client initialized")
	// No response needed for notifications
	return nil
}

// handleToolsList aggregates and returns all available tools from all backends.
func (s *StdioServer) handleToolsList(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// Wait for backend initialization to complete (up to 5 seconds)
	s.backendManager.WaitForInitialization(ctx, 5*time.Second)

	tools := s.toolRegistry.ListAllTools()

	// Add built-in proxy tools (available even with no backends)
	builtInTools := []RegisteredTool{
		{
			Name:        "proxy:detect-servers",
			Description: "Detect existing MCP servers in standard locations",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "proxy:server-status",
			Description: "Get status of currently proxied MCP servers",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "proxy:open-dashboard",
			Description: "Open the Sentinel Proxy management dashboard in your browser",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "proxy:migrate-config",
			Description: "Migrate existing MCP server configs to the Sentinel Proxy registry",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"policy_mode": map[string]interface{}{
						"type":        "string",
						"description": "Security policy mode: strict, moderate, or permissive",
						"enum":        []string{"strict", "moderate", "permissive"},
					},
				},
				"required": []string{"policy_mode"},
			},
		},
	}

	// Combine built-in tools with backend tools
	allTools := append(builtInTools, tools...)

	result := map[string]interface{}{
		"tools": allTools,
	}

	return s.makeResult(request.ID, result)
}

// handleToolsCall routes a tool call to the appropriate backend and returns the result.
func (s *StdioServer) handleToolsCall(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(request.Params, &params); err != nil {
		return s.makeError(request.ID, -32602, "Invalid params", err.Error())
	}

	// Handle built-in proxy tools
	switch params.Name {
	case "proxy:detect-servers":
		return s.handleProxyDetectServers(request.ID)
	case "proxy:server-status":
		return s.handleProxyServerStatus(request.ID)
	case "proxy:open-dashboard":
		return s.handleProxyOpenDashboard(request.ID)
	case "proxy:migrate-config":
		return s.handleProxyMigrateConfig(request.ID, params.Arguments)
	}

	// Get the backend that owns this tool
	backendID, err := s.toolRegistry.GetToolBackend(params.Name)
	if err != nil {
		s.logger.Warn("tool not found: %s", params.Name)
		return s.makeError(request.ID, -32602, "Tool not found", params.Name)
	}

	// Get the original tool name (without the backend namespace prefix)
	tool, err := s.toolRegistry.GetTool(params.Name)
	if err != nil {
		s.logger.Warn("tool metadata not found: %s", params.Name)
		return s.makeError(request.ID, -32602, "Tool not found", params.Name)
	}

	// Route to backend with the original tool name
	response, err := s.backendManager.CallTool(ctx, backendID, tool.OriginalName, params.Arguments)
	if err != nil {
		s.logger.Error("tool call failed: %v", err)
		return s.makeError(request.ID, -32603, "Tool call failed", err.Error())
	}

	return s.makeResult(request.ID, response)
}

// handleResourcesList routes to backend.
func (s *StdioServer) handleResourcesList(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// TODO: Implement resource aggregation
	return s.makeError(request.ID, -32601, "Not implemented", "resources/list")
}

// handleResourcesRead routes to backend.
func (s *StdioServer) handleResourcesRead(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// TODO: Implement resource reading
	return s.makeError(request.ID, -32601, "Not implemented", "resources/read")
}

// handlePromptsList routes to backend.
func (s *StdioServer) handlePromptsList(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// TODO: Implement prompt listing
	return s.makeError(request.ID, -32601, "Not implemented", "prompts/list")
}

// handlePromptsGet routes to backend.
func (s *StdioServer) handlePromptsGet(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// TODO: Implement prompt retrieval
	return s.makeError(request.ID, -32601, "Not implemented", "prompts/get")
}

// handleProxyDetectServers detects and returns a list of existing MCP servers.
func (s *StdioServer) handleProxyDetectServers(id interface{}) interface{} {
	detector, err := cmd.NewServerDetector()
	if err != nil {
		return s.makeError(id, -32603, "Detection failed", err.Error())
	}

	servers, err := detector.DetectAll()
	if err != nil {
		// Return partial results if some detection succeeded
		s.logger.Warn("detection partially failed: %v", err)
	}

	// Convert to JSON-serializable format
	detectedServers := make([]map[string]interface{}, len(servers))
	for i, srv := range servers {
		detectedServers[i] = map[string]interface{}{
			"name":        srv.Name,
			"type":        srv.Type,
			"command":     srv.Command,
			"url":         srv.URL,
			"args":        srv.Args,
			"env":         srv.Env,
			"source":      srv.Source,
			"description": srv.Description,
		}
	}

	result := map[string]interface{}{
		"servers": detectedServers,
		"count":   len(detectedServers),
		"message": fmt.Sprintf("Detected %d MCP server(s)", len(detectedServers)),
	}
	return s.makeResult(id, result)
}

// handleProxyServerStatus returns the status of currently proxied servers.
func (s *StdioServer) handleProxyServerStatus(id interface{}) interface{} {
	backends := s.backendManager.GetInitializedBackends()
	status := map[string]interface{}{
		"total_backends": len(backends),
		"backends":       []map[string]interface{}{},
	}

	for _, backend := range backends {
		status["backends"] = append(status["backends"].([]map[string]interface{}), map[string]interface{}{
			"name":        backend.config.Name,
			"transport":   backend.config.Transport,
			"initialized": backend.initialized,
		})
	}

	return s.makeResult(id, status)
}

// handleProxyOpenDashboard opens the dashboard in the default browser.
func (s *StdioServer) handleProxyOpenDashboard(id interface{}) interface{} {
	dashboardURL := "http://localhost:13337"

	// Open browser (platform-specific)
	var cmd *exec.Cmd
	switch {
	case os.Getenv("OSTYPE") == "linux-gnu" || os.Getenv("OSTYPE") == "linux":
		cmd = exec.Command("xdg-open", dashboardURL)
	case os.Getenv("OSTYPE") == "darwin" || os.Getenv("OSTYPE") == "darwin15":
		cmd = exec.Command("open", dashboardURL)
	default:
		// Fallback: try common browsers or just return the URL
		cmd = exec.Command("start", dashboardURL)
	}

	if err := cmd.Start(); err != nil {
		// Browser open failed, but return success with URL so user can open manually
		s.logger.Warn("failed to auto-open browser: %v", err)
	}

	result := map[string]interface{}{
		"dashboard_url": dashboardURL,
		"message":       fmt.Sprintf("Dashboard available at %s", dashboardURL),
		"status":        "opening",
	}
	return s.makeResult(id, result)
}

// handleProxyMigrateConfig updates the policy mode in the proxy config registry.
// Note: This is called after servers have been detected via proxy:detect-servers tool.
func (s *StdioServer) handleProxyMigrateConfig(id interface{}, args json.RawMessage) interface{} {
	var params struct {
		PolicyMode string                 `json:"policy_mode"`
		Servers    []DetectedServer       `json:"servers,omitempty"`
	}

	if err := json.Unmarshal(args, &params); err != nil {
		return s.makeError(id, -32602, "Invalid params", err.Error())
	}

	// Validate policy mode
	if params.PolicyMode == "" {
		params.PolicyMode = "moderate" // default
	}
	switch params.PolicyMode {
	case "strict", "moderate", "permissive":
		// Valid
	default:
		return s.makeError(id, -32602, "Invalid policy mode", "must be strict, moderate, or permissive")
	}

	// If servers were provided, migrate with them; otherwise, use current registry
	migrator, err := NewConfigMigrator()
	if err != nil {
		return s.makeError(id, -32603, "Migration failed", err.Error())
	}

	result, err := migrator.MigrateWithServers(params.Servers, params.PolicyMode)
	if err != nil && !result.Success {
		return s.makeError(id, -32603, "Migration failed", err.Error())
	}

	s.logger.Info("Config migration complete: %d server(s) migrated", result.ServersMigrated)

	// Return migration result
	return s.makeResult(id, result)
}

// handleCompletionComplete routes to backend.
func (s *StdioServer) handleCompletionComplete(ctx context.Context, request JSONRPCRequest) interface{} {
	if !s.initialized {
		return s.makeError(request.ID, -32603, "Not initialized", "Call initialize first")
	}

	// TODO: Implement completion
	return s.makeError(request.ID, -32601, "Not implemented", "completion/complete")
}

// aggregateCapabilities combines capabilities from all initialized backends.
// Even with no backends, the proxy always supports tools (at minimum the built-in ones).
func (s *StdioServer) aggregateCapabilities() *proxy.Capabilities {
	// Always advertise sampling.tools and listChanged so clients will call tools/list
	caps := &proxy.Capabilities{
		Sampling: &proxy.SamplingCapability{
			Tools: true,
		},
		Tools: &proxy.ToolsCapability{
			ListChanged: true,
		},
		ListChanged: true,
	}

	// Merge backend capabilities (logical OR: advertise anything any backend supports)
	backends := s.backendManager.GetInitializedBackends()
	if len(backends) == 0 {
		return caps
	}

	for _, backend := range backends {
		if backend.Capabilities != nil {
			if backend.Capabilities.Sampling != nil {
				if caps.Sampling == nil {
					caps.Sampling = &proxy.SamplingCapability{}
				}
				caps.Sampling.Tools = caps.Sampling.Tools || backend.Capabilities.Sampling.Tools
			}
			if backend.Capabilities.Elicitation != nil {
				if caps.Elicitation == nil {
					caps.Elicitation = &proxy.ElicitationCapability{}
				}
				caps.Elicitation.Enabled = caps.Elicitation.Enabled || backend.Capabilities.Elicitation.Enabled
			}
			if backend.Capabilities.Tools != nil {
				if caps.Tools == nil {
					caps.Tools = &proxy.ToolsCapability{}
				}
				caps.Tools.ListChanged = caps.Tools.ListChanged || backend.Capabilities.Tools.ListChanged
			}
			caps.ListChanged = caps.ListChanged || backend.Capabilities.ListChanged
			caps.Subscribe = caps.Subscribe || backend.Capabilities.Subscribe
			// Handle Logging as interface{} (can be bool or object)
			backendLogging := proxy.ToBoolean(backend.Capabilities.Logging)
			currentLogging := proxy.ToBoolean(caps.Logging)
			caps.Logging = currentLogging || backendLogging
		}
	}

	return caps
}

// Helper methods for building responses

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response or error.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error response.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (s *StdioServer) makeResult(id interface{}, result interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

func (s *StdioServer) makeError(id interface{}, code int, message string, data interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

func (s *StdioServer) sendError(id interface{}, code int, message string) error {
	return s.encoder.Encode(s.makeError(id, code, message, nil))
}

func initializeDB(dbPath string) (*sql.DB, error) {
	var db *sql.DB
	var err error

	if dbPath != "" {
		db, err = sql.Open("sqlite", "file:"+dbPath)
	} else {
		db, err = sql.Open("sqlite", "file:memdb?mode=memory&cache=shared")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	if err := initDBSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init database schema: %v", err)
	}

	return db, nil
}

func initDBSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS capabilities (
		server_id TEXT NOT NULL,
		capability TEXT NOT NULL,
		PRIMARY KEY (server_id, capability)
	);
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT,
		agent_id TEXT,
		server_id TEXT,
		method TEXT,
		capability TEXT,
		session_id TEXT,
		transport TEXT,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(schema)
	return err
}
