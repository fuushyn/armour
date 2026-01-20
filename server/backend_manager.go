package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
)

// autoDiscoverServersFromClaudeConfig reads Claude Code's config and extracts MCP servers
func (bm *BackendManager) autoDiscoverServersFromClaudeConfig() []proxy.ServerEntry {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		return nil
	}

	configPath := path.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(configPath)
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

// autoDiscoverPluginMCPServers scans Claude Code's plugins directory and discovers MCP servers
// provided by installed plugins. This enables Approach A: plugins' MCP servers are auto-discovered
// and can be proxied through Armour.
// Supports both standard plugins (plugin.json) and marketplace plugins (marketplace.json)
func (bm *BackendManager) autoDiscoverPluginMCPServers() []proxy.ServerEntry {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		return nil
	}

	// Claude Code plugins are typically stored in ~/.claude/plugins
	pluginsDir := filepath.Join(homeDir, ".claude", "plugins")

	// Check if plugins directory exists
	if _, err := os.Stat(pluginsDir); err != nil {
		bm.logger.Debug("plugins directory not found: %v", err)
		return nil
	}

	servers := []proxy.ServerEntry{}
	seenServers := make(map[string]bool)

	// Walk through plugins directory structure
	// Plugins can be in: ~/.claude/plugins/*/  or ~/.claude/plugins/marketplaces/*/*/
	err := filepath.Walk(pluginsDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on errors
		}

		parentDir := filepath.Base(filepath.Dir(path))

		// Look for .claude-plugin/plugin.json files (standard plugins)
		if info.Name() == "plugin.json" && parentDir == ".claude-plugin" {
			if pluginServers := bm.parsePluginMCPServers(path, seenServers); pluginServers != nil {
				servers = append(servers, pluginServers...)
			}
		}

		// Look for .claude-plugin/marketplace.json files (marketplace plugins)
		if info.Name() == "marketplace.json" && parentDir == ".claude-plugin" {
			if pluginServers := bm.parseMarketplacePluginMCPServers(path, seenServers); pluginServers != nil {
				servers = append(servers, pluginServers...)
			}
		}

		return nil
	})

	if err != nil {
		bm.logger.Debug("error scanning plugins directory: %v", err)
	}

	if len(servers) > 0 {
		bm.logger.Info("auto-discovered %d MCP servers from plugins", len(servers))
	}

	return servers
}

// parsePluginMCPServers parses a single plugin's plugin.json and extracts mcpServers declarations
func (bm *BackendManager) parsePluginMCPServers(pluginJSONPath string, seenServers map[string]bool) []proxy.ServerEntry {
	data, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		bm.logger.Debug("failed to read plugin.json at %s: %v", pluginJSONPath, err)
		return nil
	}

	var pluginManifest struct {
		Name       string      `json:"name"`
		MCPServers interface{} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &pluginManifest); err != nil {
		bm.logger.Debug("failed to parse plugin.json at %s: %v", pluginJSONPath, err)
		return nil
	}

	var servers []proxy.ServerEntry
	pluginRoot := filepath.Dir(filepath.Dir(pluginJSONPath))

	// Process each MCP server declared in the plugin
	switch mcpServers := pluginManifest.MCPServers.(type) {
	case map[string]interface{}:
		for serverName, serverRaw := range mcpServers {
			serverConfig, ok := serverRaw.(map[string]interface{})
			if !ok {
				bm.logger.Debug("plugin %s: MCP server '%s' has invalid config shape", pluginManifest.Name, serverName)
				continue
			}
			servers = append(servers, bm.buildServerEntry(pluginManifest.Name, serverName, serverConfig, seenServers, pluginRoot)...)
		}
	case []interface{}:
		for _, serverRaw := range mcpServers {
			serverConfig, ok := serverRaw.(map[string]interface{})
			if !ok {
				bm.logger.Debug("plugin %s: MCP server entry has invalid config shape", pluginManifest.Name)
				continue
			}
			serverName, ok := serverConfig["name"].(string)
			if !ok {
				bm.logger.Debug("plugin %s: MCP server missing 'name' field, skipping", pluginManifest.Name)
				continue
			}
			servers = append(servers, bm.buildServerEntry(pluginManifest.Name, serverName, serverConfig, seenServers, pluginRoot)...)
		}
	case string:
		mcpPath := filepath.Join(pluginRoot, mcpServers)
		data, err := os.ReadFile(mcpPath)
		if err != nil {
			bm.logger.Debug("failed to read mcpServers path %s: %v", mcpPath, err)
			return servers
		}
		var mcpConfig struct {
			MCPServers map[string]interface{} `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &mcpConfig); err != nil {
			bm.logger.Debug("failed to parse mcpServers file %s: %v", mcpPath, err)
			return servers
		}
		for serverName, serverRaw := range mcpConfig.MCPServers {
			serverConfig, ok := serverRaw.(map[string]interface{})
			if !ok {
				bm.logger.Debug("plugin %s: MCP server '%s' has invalid config shape", pluginManifest.Name, serverName)
				continue
			}
			servers = append(servers, bm.buildServerEntry(pluginManifest.Name, serverName, serverConfig, seenServers, pluginRoot)...)
		}
	default:
		return nil // Plugin doesn't provide MCP servers
	}

	return servers
}

func (bm *BackendManager) buildServerEntry(pluginName string, serverName string, serverConfig map[string]interface{}, seenServers map[string]bool, pluginRoot string) []proxy.ServerEntry {
	if serverName == "" {
		bm.logger.Debug("plugin %s: MCP server missing name, skipping", pluginName)
		return nil
	}

	// Skip if we've already seen this server (avoid duplicates)
	if seenServers[serverName] {
		bm.logger.Debug("plugin %s: skipping duplicate MCP server '%s'", pluginName, serverName)
		return nil
	}

	// Skip Armour's own MCP server to avoid circular references
	if serverName == "armour" || serverName == "mcp-go-proxy" {
		bm.logger.Debug("plugin %s: skipping Armour's own MCP server", pluginName)
		return nil
	}

	seenServers[serverName] = true

	// Convert plugin server config to ServerEntry
	entry := bm.convertPluginMCPServerToEntry(serverName, serverConfig, pluginName, pluginRoot)
	if entry != nil {
		bm.logger.Debug("discovered MCP server '%s' from plugin '%s' (%s)",
			serverName, pluginName, entry.Transport)
		return []proxy.ServerEntry{*entry}
	}
	return nil
}

// convertPluginMCPServerToEntry converts a plugin's MCP server declaration to a ServerEntry
func (bm *BackendManager) convertPluginMCPServerToEntry(serverName string, config map[string]interface{}, pluginName string, pluginRoot string) *proxy.ServerEntry {
	entry := &proxy.ServerEntry{
		Name: serverName,
	}

	// Determine transport type
	transportType, hasType := config["type"].(string)
	if !hasType {
		// Try to infer from config
		if _, hasURL := config["url"]; hasURL {
			transportType = "http"
		} else if _, hasCommand := config["command"]; hasCommand {
			transportType = "stdio"
		} else {
			bm.logger.Debug("cannot determine transport for plugin MCP server '%s'", serverName)
			return nil
		}
	}
	entry.Transport = transportType

	// Extract transport-specific fields
	switch transportType {
	case "http":
		if url, ok := config["url"].(string); ok {
			entry.URL = url
		} else {
			bm.logger.Debug("HTTP MCP server '%s' missing 'url' field", serverName)
			return nil
		}

	case "sse":
		if url, ok := config["url"].(string); ok {
			entry.URL = url
		} else {
			bm.logger.Debug("SSE MCP server '%s' missing 'url' field", serverName)
			return nil
		}

	case "stdio":
		if cmd, ok := config["command"].(string); ok {
			entry.Command = cmd
		} else {
			bm.logger.Debug("stdio MCP server '%s' missing 'command' field", serverName)
			return nil
		}

		// Extract args if present
		if args, ok := config["args"].([]interface{}); ok {
			entry.Args = make([]string, 0, len(args))
			for _, arg := range args {
				if argStr, ok := arg.(string); ok {
					entry.Args = append(entry.Args, argStr)
				}
			}
		}

	default:
		bm.logger.Debug("unsupported transport type '%s' for plugin MCP server '%s'", transportType, serverName)
		return nil
	}

	// Extract headers if present
	if headers, ok := config["headers"].(map[string]interface{}); ok {
		entry.Headers = make(map[string]string)
		for k, v := range headers {
			if headerVal, ok := v.(string); ok {
				entry.Headers[k] = headerVal
			}
		}
	}

	// Extract environment variables if present
	if env, ok := config["env"].(map[string]interface{}); ok {
		entry.Env = make(map[string]string)
		for k, v := range env {
			if envVal, ok := v.(string); ok {
				entry.Env[k] = envVal
			}
		}
	}

	applyPluginContext(entry, pluginName, pluginRoot)

	return entry
}

// parseMarketplacePluginMCPServers parses marketplace.json files for MCP servers.
// Marketplace files are similar to plugin.json files but used for marketplace plugins.
func (bm *BackendManager) parseMarketplacePluginMCPServers(marketplaceJSONPath string, seenServers map[string]bool) []proxy.ServerEntry {
	data, err := os.ReadFile(marketplaceJSONPath)
	if err != nil {
		bm.logger.Debug("failed to read marketplace.json at %s: %v", marketplaceJSONPath, err)
		return nil
	}

	var marketplaceManifest struct {
		Plugins []struct {
			Name       string      `json:"name"`
			MCPServers interface{} `json:"mcpServers"`
			Source     interface{} `json:"source"`
		} `json:"plugins"`
	}

	if err := json.Unmarshal(data, &marketplaceManifest); err != nil {
		bm.logger.Debug("failed to parse marketplace.json at %s: %v", marketplaceJSONPath, err)
		return nil
	}

	pluginRoot := filepath.Dir(filepath.Dir(marketplaceJSONPath))
	pluginDirName := filepath.Base(pluginRoot)
	var servers []proxy.ServerEntry

	for _, plugin := range marketplaceManifest.Plugins {
		if plugin.Name != "" && plugin.Name != pluginDirName {
			continue
		}

		baseDir := pluginRoot
		if sourcePath, ok := plugin.Source.(string); ok {
			if sourcePath != "" && !filepath.IsAbs(sourcePath) && !strings.Contains(sourcePath, "://") {
				baseDir = filepath.Join(pluginRoot, sourcePath)
			}
		}

		if plugin.MCPServers == nil {
			continue
		}

		switch mcpServers := plugin.MCPServers.(type) {
		case map[string]interface{}:
			for serverName, serverRaw := range mcpServers {
				serverConfig, ok := serverRaw.(map[string]interface{})
				if !ok {
					bm.logger.Debug("marketplace plugin %s: MCP server '%s' has invalid config shape", plugin.Name, serverName)
					continue
				}
				servers = append(servers, bm.buildServerEntry(plugin.Name, serverName, serverConfig, seenServers, baseDir)...)
			}
		case []interface{}:
			for _, serverRaw := range mcpServers {
				serverConfig, ok := serverRaw.(map[string]interface{})
				if !ok {
					bm.logger.Debug("marketplace plugin %s: MCP server entry has invalid config shape", plugin.Name)
					continue
				}
				serverName, _ := serverConfig["name"].(string)
				if serverName == "" {
					bm.logger.Debug("marketplace plugin %s: MCP server missing 'name' field, skipping", plugin.Name)
					continue
				}
				servers = append(servers, bm.buildServerEntry(plugin.Name, serverName, serverConfig, seenServers, baseDir)...)
			}
		case string:
			mcpPath := filepath.Join(baseDir, mcpServers)
			data, err := os.ReadFile(mcpPath)
			if err != nil {
				bm.logger.Debug("failed to read marketplace mcpServers path %s: %v", mcpPath, err)
				continue
			}
			var mcpConfig struct {
				MCPServers map[string]interface{} `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &mcpConfig); err != nil {
				bm.logger.Debug("failed to parse marketplace mcpServers file %s: %v", mcpPath, err)
				continue
			}
			for serverName, serverRaw := range mcpConfig.MCPServers {
				serverConfig, ok := serverRaw.(map[string]interface{})
				if !ok {
					bm.logger.Debug("marketplace plugin %s: MCP server '%s' has invalid config shape", plugin.Name, serverName)
					continue
				}
				servers = append(servers, bm.buildServerEntry(plugin.Name, serverName, serverConfig, seenServers, baseDir)...)
			}
		default:
			bm.logger.Debug("marketplace plugin %s: unsupported mcpServers format", plugin.Name)
		}
	}

	return servers
}

// discoverAndMergePluginServers discovers MCP servers from plugins and merges them with configured servers.
// This implements Approach A where plugin MCP servers are auto-discovered and proxied through Armour.
// Discovered servers don't override explicitly configured servers (configured servers take priority).
func (bm *BackendManager) discoverAndMergePluginServers() {
	if bm.registry == nil {
		bm.logger.Debug("registry is nil, skipping plugin discovery")
		return
	}

	// First, discover servers from Claude Code config
	configServers := bm.autoDiscoverServersFromClaudeConfig()

	// Then, discover servers from installed plugins
	pluginServers := bm.autoDiscoverPluginMCPServers()

	if len(configServers) == 0 && len(pluginServers) == 0 {
		bm.logger.Debug("no servers discovered from config or plugins")
		return
	}

	// Track which server names we already have (to avoid duplicates)
	existingNames := make(map[string]bool)
	for _, srv := range bm.registry.Servers {
		existingNames[srv.Name] = true
	}

	// Add discovered config servers that aren't already in registry
	for _, srv := range configServers {
		if !existingNames[srv.Name] {
			bm.registry.Servers = append(bm.registry.Servers, srv)
			existingNames[srv.Name] = true
			bm.logger.Debug("added discovered server from config: %s (%s)", srv.Name, srv.Transport)
		}
	}

	// Add plugin servers that aren't already in registry
	for _, srv := range pluginServers {
		if !existingNames[srv.Name] {
			bm.registry.Servers = append(bm.registry.Servers, srv)
			existingNames[srv.Name] = true
			bm.logger.Debug("added discovered server from plugin: %s (%s)", srv.Name, srv.Transport)
		}
	}

	totalDiscovered := len(configServers) + len(pluginServers)
	if totalDiscovered > 0 {
		bm.logger.Info("discovered %d total servers (%d from config, %d from plugins)",
			totalDiscovered, len(configServers), len(pluginServers))
	}
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
	registry           *proxy.ServerRegistry
	logger             *proxy.Logger
	connections        map[string]*BackendConnection
	mu                 sync.RWMutex
	toolRegistry       *ToolRegistry
	initializationDone chan struct{}
	initializationOnce sync.Once
}

const backendInitTimeout = 8 * time.Second

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
// First, it discovers MCP servers from installed Claude Code plugins (Approach A support).
// Then, it initializes all servers (configured + discovered).
// Partial failures are logged but don't prevent initialization of other backends.
func (bm *BackendManager) Initialize(ctx context.Context) error {
	defer bm.initializationOnce.Do(func() {
		close(bm.initializationDone)
	})

	bm.mu.Lock()
	// First, discover and merge plugin MCP servers with configured servers
	bm.discoverAndMergePluginServers()

	if bm.registry == nil || len(bm.registry.Servers) == 0 {
		bm.mu.Unlock()
		bm.logger.Debug("no backend servers configured in registry, operating with empty backend list")
		bm.logger.Debug("use proxy:detect-servers and proxy:migrate-config tools to add servers")
		return nil
	}

	servers := make([]proxy.ServerEntry, len(bm.registry.Servers))
	copy(servers, bm.registry.Servers)
	bm.mu.Unlock()

	var initErrors []error
	var initErrMu sync.Mutex
	var wg sync.WaitGroup

	for i := range servers {
		entry := servers[i]
		wg.Add(1)
		go func(serverEntry proxy.ServerEntry) {
			defer wg.Done()
			initCtx, cancel := context.WithTimeout(ctx, backendInitTimeout)
			defer cancel()

			if err := bm.initializeBackend(initCtx, &serverEntry); err != nil {
				bm.logger.Error("failed to initialize backend %s: %v", serverEntry.Name, err)
				initErrMu.Lock()
				initErrors = append(initErrors, err)
				initErrMu.Unlock()
			}
		}(entry)
	}

	wg.Wait()

	bm.mu.RLock()
	initialized := len(bm.connections)
	bm.mu.RUnlock()

	// If we initialized at least one backend, consider it a success
	if initialized > 0 {
		bm.logger.Info("initialized %d backend servers", initialized)
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

// WaitForAnyBackend waits for at least one backend to initialize, or times out.
func (bm *BackendManager) WaitForAnyBackend(ctx context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		bm.mu.RLock()
		ready := len(bm.connections) > 0
		bm.mu.RUnlock()

		if ready {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// initializeBackend initializes a single backend server.
func (bm *BackendManager) initializeBackend(ctx context.Context, serverEntry *proxy.ServerEntry) error {
	expandServerEntry(serverEntry)
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
	bm.mu.Lock()
	bm.connections[serverEntry.Name] = conn
	bm.mu.Unlock()

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
			Code    int    `json:"code"`
			Message string `json:"message"`
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

// ListResources calls resources/list on a backend and returns the list of resources
func (bm *BackendManager) ListResources(ctx context.Context, backendID string) ([]interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/list",
		"params":  map[string]interface{}{},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Resources []interface{} `json:"resources"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse resources/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result.Resources, nil
}

// ReadResource calls resources/read on a backend
func (bm *BackendManager) ReadResource(ctx context.Context, backendID, uri string) (interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": uri,
		},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Contents interface{} `json:"contents"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse resources/read response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result.Contents, nil
}

// ListResourceTemplates calls resources/templates/list on a backend
func (bm *BackendManager) ListResourceTemplates(ctx context.Context, backendID string) ([]interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/templates/list",
		"params":  map[string]interface{}{},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			ResourceTemplates []interface{} `json:"resourceTemplates"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse resources/templates/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result.ResourceTemplates, nil
}

// SubscribeToResource calls resources/subscribe on a backend
func (bm *BackendManager) SubscribeToResource(ctx context.Context, backendID, uri string) error {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/subscribe",
		"params": map[string]interface{}{
			"uri": uri,
		},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return err
	}

	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("failed to parse resources/subscribe response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return nil
}

// UnsubscribeFromResource calls resources/unsubscribe on a backend
func (bm *BackendManager) UnsubscribeFromResource(ctx context.Context, backendID, uri string) error {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/unsubscribe",
		"params": map[string]interface{}{
			"uri": uri,
		},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return err
	}

	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("failed to parse resources/unsubscribe response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return nil
}

// ListPrompts calls prompts/list on a backend
func (bm *BackendManager) ListPrompts(ctx context.Context, backendID string) ([]interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/list",
		"params":  map[string]interface{}{},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Prompts []interface{} `json:"prompts"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse prompts/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result.Prompts, nil
}

// GetPrompt calls prompts/get on a backend
func (bm *BackendManager) GetPrompt(ctx context.Context, backendID, name string, arguments map[string]interface{}) (interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": arguments,
		},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse prompts/get response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result, nil
}

// GetCompletion calls completion/complete on a backend
func (bm *BackendManager) GetCompletion(ctx context.Context, backendID, ref string, argument, metadata interface{}) (interface{}, error) {
	bm.mu.RLock()
	conn, exists := bm.connections[backendID]
	bm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("backend not found: %s", backendID)
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "completion/complete",
		"params": map[string]interface{}{
			"ref":      ref,
			"argument": argument,
			"_meta":    metadata,
		},
	}

	respBytes, err := conn.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse completion/complete response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}

	return resp.Result, nil
}
