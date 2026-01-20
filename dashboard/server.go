package dashboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/user/mcp-go-proxy/proxy"
	"github.com/user/mcp-go-proxy/server"
)

// Server provides a web-based dashboard for managing the MCP proxy.
type Server struct {
	listenAddr string
	httpServer *http.Server
	listener   net.Listener
	configPath string

	// References to proxy components
	registry      *proxy.ServerRegistry
	statsTracker  *server.StatsTracker
	policyManager *server.PolicyManager
	blocklist     *server.BlocklistMiddleware
	db            *sql.DB
	logger        *proxy.Logger

	mu sync.RWMutex
}

// NewDashboardServer creates a new dashboard server.
func NewDashboardServer(
	listenAddr string,
	registry *proxy.ServerRegistry,
	configPath string,
	statsTracker *server.StatsTracker,
	policyManager *server.PolicyManager,
	blocklist *server.BlocklistMiddleware,
	db *sql.DB,
	logger *proxy.Logger,
) *Server {
	ds := &Server{
		listenAddr:    listenAddr,
		registry:      registry,
		configPath:    configPath,
		statsTracker:  statsTracker,
		policyManager: policyManager,
		blocklist:     blocklist,
		db:            db,
		logger:        logger,
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/servers", ds.handleServersAPI)
	mux.HandleFunc("/api/servers/", ds.handleServerDetailAPI)
	mux.HandleFunc("/api/policy", ds.handlePolicyAPI)
	mux.HandleFunc("/api/permissions", ds.handlePermissionsAPI)
	mux.HandleFunc("/api/blocklist", ds.handleBlocklistAPI)
	mux.HandleFunc("/api/stats", ds.handleStatsAPI)
	mux.HandleFunc("/api/audit", ds.handleAuditAPI)
	mux.HandleFunc("/api/health", ds.handleHealthAPI)

	// UI endpoints
	mux.HandleFunc("/", ds.handleDashboardUI)
	mux.HandleFunc("/dashboard", ds.handleDashboardUI)
	mux.HandleFunc("/blocklist", ds.handleBlocklistUI)
	mux.HandleFunc("/audit", ds.handleAuditUI)
	mux.HandleFunc("/settings", ds.handleSettingsUI)

	ds.httpServer = &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	return ds
}

// Start starts the dashboard server.
func (ds *Server) Start() error {
	listener, err := net.Listen("tcp", ds.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", ds.listenAddr, err)
	}

	ds.listener = listener
	ds.logger.Info("dashboard server started on http://%s", listener.Addr())

	go func() {
		if err := ds.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			ds.logger.Error("dashboard server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the dashboard server.
func (ds *Server) Stop() error {
	if ds.httpServer != nil {
		return ds.httpServer.Close()
	}
	return nil
}

// API Handlers

// handleServersAPI lists all configured servers and registers new ones.
func (ds *Server) handleServersAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ds.handleListServers(w)
	case http.MethodPost:
		ds.handleRegisterServer(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ds *Server) handleListServers(w http.ResponseWriter) {
	ds.mu.RLock()
	var servers []proxy.ServerEntry
	if ds.registry != nil {
		servers = append([]proxy.ServerEntry{}, ds.registry.Servers...)
	}
	ds.mu.RUnlock()

	response := map[string]interface{}{
		"count":   len(servers),
		"servers": servers,
		"path":    ds.configPath,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ds *Server) handleRegisterServer(w http.ResponseWriter, r *http.Request) {
	if ds.configPath == "" {
		http.Error(w, "Server registration unavailable: start proxy with -config to persist servers.json", http.StatusBadRequest)
		return
	}

	var req struct {
		Name      string            `json:"name"`
		Transport string            `json:"transport"`
		URL       string            `json:"url"`
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		Env       map[string]string `json:"env"`
		Headers   map[string]string `json:"headers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "Server name required", http.StatusBadRequest)
		return
	}

	transport := strings.ToLower(strings.TrimSpace(req.Transport))
	switch transport {
	case "http", "sse":
		if strings.TrimSpace(req.URL) == "" {
			http.Error(w, "URL required for http/sse servers", http.StatusBadRequest)
			return
		}
	case "stdio":
		if strings.TrimSpace(req.Command) == "" {
			http.Error(w, "Command required for stdio servers", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "Transport must be http, stdio, or sse", http.StatusBadRequest)
		return
	}

	entry := proxy.ServerEntry{
		Name:      name,
		Transport: transport,
		URL:       strings.TrimSpace(req.URL),
		Command:   strings.TrimSpace(req.Command),
		Args:      req.Args,
		Env:       req.Env,
		Headers:   req.Headers,
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.registry == nil {
		ds.registry = &proxy.ServerRegistry{}
	}

	for _, existing := range ds.registry.Servers {
		if strings.EqualFold(existing.Name, entry.Name) {
			http.Error(w, "Server name already exists", http.StatusConflict)
			return
		}
	}

	updatedServers := append([]proxy.ServerEntry{}, ds.registry.Servers...)
	updatedServers = append(updatedServers, entry)

	if err := proxy.SaveServerRegistry(&proxy.ServerRegistry{Servers: updatedServers}, ds.configPath); err != nil {
		ds.logger.Error("failed to save server registry: %v", err)
		http.Error(w, "Failed to save server registry", http.StatusInternalServerError)
		return
	}

	ds.registry.Servers = updatedServers
	ds.logger.Info("registered new MCP server: %s (%s)", entry.Name, entry.Transport)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"server":  entry,
		"count":   len(updatedServers),
		"servers": updatedServers,
		"path":    ds.configPath,
	})
}

// handleServerDetailAPI handles individual server details and actions.
func (ds *Server) handleServerDetailAPI(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Path[len("/api/servers/"):]

	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	ds.mu.RLock()
	server := ds.registry.GetServer(serverID)
	ds.mu.RUnlock()

	if server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Return server details
		response := map[string]interface{}{
			"server": server,
			"status": "running", // TODO: Track actual status
		}
		json.NewEncoder(w).Encode(response)

	case http.MethodPut:
		// Update server configuration
		http.Error(w, "Not implemented", http.StatusNotImplemented)

	case http.MethodDelete:
		// Remove server (not actually delete, just disable)
		http.Error(w, "Not implemented", http.StatusNotImplemented)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePolicyAPI gets/sets the policy mode.
func (ds *Server) handlePolicyAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Return current policy
		mode := ds.policyManager.GetMode()
		desc := ds.policyManager.GetDescription()

		response := map[string]interface{}{
			"mode":        mode,
			"description": desc,
		}

		json.NewEncoder(w).Encode(response)

	case http.MethodPut:
		// Update policy
		var req struct {
			Mode string `json:"mode"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := ds.policyManager.SetMode(server.PolicyMode(req.Mode)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		response := map[string]string{
			"status": "success",
			"mode":   req.Mode,
		}

		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePermissionsAPI manages native tool permission rules in Claude settings.json.
func (ds *Server) handlePermissionsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	settingsPath, err := claudeSettingsPath()
	if err != nil {
		ds.logger.Error("failed to resolve settings path: %v", err)
		http.Error(w, "Settings path not available", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := loadSettings(settingsPath)
		if err != nil {
			ds.logger.Error("failed to load settings: %v", err)
			http.Error(w, "Failed to load settings", http.StatusInternalServerError)
			return
		}

		permissions := extractPermissions(settings)
		response := map[string]interface{}{
			"path":        settingsPath,
			"permissions": permissions,
		}
		json.NewEncoder(w).Encode(response)

	case http.MethodPut:
		var req struct {
			Rules []struct {
				Tool string `json:"tool"`
				Mode string `json:"mode"`
			} `json:"rules"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		settings, err := loadSettings(settingsPath)
		if err != nil {
			ds.logger.Error("failed to load settings: %v", err)
			http.Error(w, "Failed to load settings", http.StatusInternalServerError)
			return
		}

		updated, err := applyPermissionRules(settings, req.Rules)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := saveSettings(settingsPath, updated); err != nil {
			ds.logger.Error("failed to save settings: %v", err)
			http.Error(w, "Failed to save settings", http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"status": "success",
			"path":   settingsPath,
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBlocklistAPI manages blocklist rules (CRUD operations).
func (ds *Server) handleBlocklistAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract rule ID from query parameter if present
	ruleIDStr := r.URL.Query().Get("id")

	switch r.Method {
	case http.MethodGet:
		if ruleIDStr != "" {
			ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
			if err != nil {
				http.Error(w, "Invalid rule ID", http.StatusBadRequest)
				return
			}

			rule, err := server.GetBlocklistRuleByID(ds.db, ruleID)
			if err != nil {
				ds.logger.Error("failed to get blocklist rule: %v", err)
				http.Error(w, "Rule not found", http.StatusNotFound)
				return
			}

			json.NewEncoder(w).Encode(rule)
			return
		}

		// List all rules
		rules, err := server.GetAllBlocklistRules(ds.db)
		if err != nil {
			ds.logger.Error("failed to get blocklist rules: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"count": len(rules),
			"rules": rules,
		}

		json.NewEncoder(w).Encode(response)

	case http.MethodPost:
		// Create new rule
		var req struct {
			Pattern     string              `json:"pattern"`
			Description string              `json:"description,omitempty"`
			Action      string              `json:"action"`
			IsRegex     bool                `json:"is_regex"`
			IsSemantic  bool                `json:"is_semantic"`
			Tools       string              `json:"tools"`
			Enabled     *bool               `json:"enabled,omitempty"`
			Permissions *server.Permissions `json:"permissions,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		pattern := strings.TrimSpace(req.Pattern)
		if pattern == "" {
			http.Error(w, "Pattern required", http.StatusBadRequest)
			return
		}

		action, err := normalizeBlocklistAction(req.Action)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		rule := &server.BlocklistRule{
			Pattern:     pattern,
			Description: req.Description,
			Action:      action,
			IsRegex:     req.IsRegex,
			IsSemantic:  req.IsSemantic,
			Tools:       req.Tools,
			Permissions: normalizePermissions(action, req.Permissions),
			Enabled:     enabled,
		}

		if err := server.CreateBlocklistRule(ds.db, rule); err != nil {
			ds.logger.Error("failed to create blocklist rule: %v", err)
			http.Error(w, "Failed to create rule", http.StatusInternalServerError)
			return
		}

		// Refresh cache
		if ds.blocklist != nil {
			if err := ds.blocklist.RefreshRulesCache(); err != nil {
				ds.logger.Warn("failed to refresh cache: %v", err)
			}
		}

		json.NewEncoder(w).Encode(rule)

	case http.MethodPut:
		// Update existing rule
		if ruleIDStr == "" {
			http.Error(w, "Rule ID required", http.StatusBadRequest)
			return
		}

		ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid rule ID", http.StatusBadRequest)
			return
		}

		var req struct {
			Pattern     string              `json:"pattern"`
			Description string              `json:"description,omitempty"`
			Action      string              `json:"action"`
			IsRegex     bool                `json:"is_regex"`
			IsSemantic  bool                `json:"is_semantic"`
			Tools       string              `json:"tools"`
			Enabled     *bool               `json:"enabled,omitempty"`
			Permissions *server.Permissions `json:"permissions,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		pattern := strings.TrimSpace(req.Pattern)
		if pattern == "" {
			http.Error(w, "Pattern required", http.StatusBadRequest)
			return
		}

		action, err := normalizeBlocklistAction(req.Action)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		existingRule, err := server.GetBlocklistRuleByID(ds.db, ruleID)
		if err != nil {
			ds.logger.Error("failed to get blocklist rule: %v", err)
			http.Error(w, "Rule not found", http.StatusNotFound)
			return
		}

		enabled := existingRule.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		permissions := existingRule.Permissions
		if req.Permissions != nil {
			permissions = normalizePermissions(action, req.Permissions)
		}

		rule := &server.BlocklistRule{
			ID:          ruleID,
			Pattern:     pattern,
			Description: req.Description,
			Action:      action,
			IsRegex:     req.IsRegex,
			IsSemantic:  req.IsSemantic,
			Tools:       req.Tools,
			Permissions: permissions,
			Enabled:     enabled,
		}

		if err := server.UpdateBlocklistRule(ds.db, rule); err != nil {
			ds.logger.Error("failed to update blocklist rule: %v", err)
			http.Error(w, "Failed to update rule", http.StatusInternalServerError)
			return
		}

		// Refresh cache
		if ds.blocklist != nil {
			if err := ds.blocklist.RefreshRulesCache(); err != nil {
				ds.logger.Warn("failed to refresh cache: %v", err)
			}
		}

		json.NewEncoder(w).Encode(rule)

	case http.MethodDelete:
		// Delete rule
		if ruleIDStr == "" {
			http.Error(w, "Rule ID required", http.StatusBadRequest)
			return
		}

		ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid rule ID", http.StatusBadRequest)
			return
		}

		if err := server.DeleteBlocklistRule(ds.db, ruleID); err != nil {
			ds.logger.Error("failed to delete blocklist rule: %v", err)
			http.Error(w, "Failed to delete rule", http.StatusInternalServerError)
			return
		}

		// Refresh cache
		if ds.blocklist != nil {
			if err := ds.blocklist.RefreshRulesCache(); err != nil {
				ds.logger.Warn("failed to refresh cache: %v", err)
			}
		}

		response := map[string]string{
			"status": "deleted",
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func normalizeBlocklistAction(action string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if normalized == "" {
		return "", fmt.Errorf("Action required")
	}
	if normalized != "block" && normalized != "allow" {
		return "", fmt.Errorf("Invalid action: %s", action)
	}
	return normalized, nil
}

func normalizePermissions(action string, perms *server.Permissions) server.Permissions {
	defaults := server.DefaultPermissions(action)
	if perms == nil {
		return defaults
	}

	merged := *perms
	if merged.ToolsCall == "" {
		merged.ToolsCall = defaults.ToolsCall
	}
	if merged.ToolsList == "" {
		merged.ToolsList = defaults.ToolsList
	}
	if merged.ResourcesRead == "" {
		merged.ResourcesRead = defaults.ResourcesRead
	}
	if merged.ResourcesList == "" {
		merged.ResourcesList = defaults.ResourcesList
	}
	if merged.ResourcesSubscribe == "" {
		merged.ResourcesSubscribe = defaults.ResourcesSubscribe
	}
	if merged.PromptsGet == "" {
		merged.PromptsGet = defaults.PromptsGet
	}
	if merged.PromptsList == "" {
		merged.PromptsList = defaults.PromptsList
	}
	if merged.Sampling == "" {
		merged.Sampling = defaults.Sampling
	}
	return merged
}

func claudeSettingsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".claude", "settings.json"), nil
}

func loadSettings(path string) (map[string]interface{}, error) {
	settings := make(map[string]interface{})
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		return settings, nil
	}

	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func saveSettings(path string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func extractPermissions(settings map[string]interface{}) map[string][]string {
	permissions := make(map[string][]string)
	raw, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		return permissions
	}

	permissions["allow"] = interfaceToStringSlice(raw["allow"])
	permissions["ask"] = interfaceToStringSlice(raw["ask"])
	permissions["deny"] = interfaceToStringSlice(raw["deny"])
	return permissions
}

func applyPermissionRules(settings map[string]interface{}, rules []struct {
	Tool string `json:"tool"`
	Mode string `json:"mode"`
}) (map[string]interface{}, error) {
	if len(rules) == 0 {
		return settings, nil
	}

	permissions, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		permissions = make(map[string]interface{})
	}

	allow := interfaceToStringSlice(permissions["allow"])
	ask := interfaceToStringSlice(permissions["ask"])
	deny := interfaceToStringSlice(permissions["deny"])

	for _, rule := range rules {
		tool := strings.TrimSpace(rule.Tool)
		if tool == "" {
			continue
		}

		mode := strings.ToLower(strings.TrimSpace(rule.Mode))
		if mode != "" && mode != "allow" && mode != "ask" && mode != "deny" && mode != "unset" {
			return nil, fmt.Errorf("invalid mode for tool %s", tool)
		}

		allow = removeExactRule(allow, tool)
		ask = removeExactRule(ask, tool)
		deny = removeExactRule(deny, tool)

		switch mode {
		case "allow":
			allow = appendUnique(allow, tool)
		case "ask":
			ask = appendUnique(ask, tool)
		case "deny":
			deny = appendUnique(deny, tool)
		}
	}

	if len(allow) > 0 {
		permissions["allow"] = allow
	} else {
		delete(permissions, "allow")
	}
	if len(ask) > 0 {
		permissions["ask"] = ask
	} else {
		delete(permissions, "ask")
	}
	if len(deny) > 0 {
		permissions["deny"] = deny
	} else {
		delete(permissions, "deny")
	}

	if len(permissions) > 0 {
		settings["permissions"] = permissions
	} else {
		delete(settings, "permissions")
	}

	return settings, nil
}

func interfaceToStringSlice(raw interface{}) []string {
	if raw == nil {
		return nil
	}

	switch value := raw.(type) {
	case []string:
		return append([]string{}, value...)
	case []interface{}:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func removeExactRule(rules []string, target string) []string {
	if len(rules) == 0 {
		return rules
	}
	filtered := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule != target {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

func appendUnique(rules []string, value string) []string {
	for _, rule := range rules {
		if rule == value {
			return rules
		}
	}
	return append(rules, value)
}

// handleStatsAPI returns statistics and KPIs.
func (ds *Server) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := ds.statsTracker.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAuditAPI returns audit log entries.
func (ds *Server) handleAuditAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Implement audit log retrieval
	response := map[string]interface{}{
		"entries": []interface{}{},
		"count":   0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealthAPI returns health status.
func (ds *Server) handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status":  "ok",
		"version": "1.0.15",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UI Handlers

// handleDashboardUI serves the main dashboard page.
func (ds *Server) handleDashboardUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getUnifiedDashboardHTML())
}

// handleAuditUI serves the audit log page.
func (ds *Server) handleAuditUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getUnifiedDashboardHTML())
}

// handleBlocklistUI serves the blocklist management page.
func (ds *Server) handleBlocklistUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getUnifiedDashboardHTML())
}

// handleSettingsUI serves the settings page.
func (ds *Server) handleSettingsUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getUnifiedDashboardHTML())
}

// HTML Templates

func getDashboardHTML() string {
	return `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>MCP Proxy Dashboard</title>
	<style>
		* {
			margin: 0;
			padding: 0;
			box-sizing: border-box;
		}

		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
			min-height: 100vh;
			padding: 20px;
		}

		.container {
			max-width: 1200px;
			margin: 0 auto;
		}

		header {
			background: white;
			padding: 30px;
			border-radius: 10px;
			margin-bottom: 30px;
			box-shadow: 0 4px 6px rgba(0,0,0,0.1);
		}

		h1 {
			color: #333;
			margin-bottom: 10px;
		}

		.stats-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
			gap: 20px;
			margin-bottom: 30px;
		}

		.stat-card {
			background: white;
			padding: 20px;
			border-radius: 10px;
			box-shadow: 0 4px 6px rgba(0,0,0,0.1);
		}

		.stat-card h3 {
			color: #667eea;
			font-size: 12px;
			text-transform: uppercase;
			margin-bottom: 10px;
		}

		.stat-value {
			font-size: 32px;
			font-weight: bold;
			color: #333;
		}

		.servers-section {
			background: white;
			padding: 30px;
			border-radius: 10px;
			box-shadow: 0 4px 6px rgba(0,0,0,0.1);
		}

		.servers-section h2 {
			color: #333;
			margin-bottom: 20px;
			border-bottom: 2px solid #667eea;
			padding-bottom: 10px;
		}

		.server-list {
			display: grid;
			gap: 15px;
		}

		.server-item {
			background: #f8f9fa;
			padding: 15px;
			border-radius: 8px;
			border-left: 4px solid #667eea;
			display: flex;
			justify-content: space-between;
			align-items: center;
		}

		.server-info h3 {
			color: #333;
			margin-bottom: 5px;
		}

		.server-info p {
			color: #666;
			font-size: 14px;
		}

		.server-status {
			padding: 6px 12px;
			border-radius: 6px;
			background: #d4edda;
			color: #155724;
			font-size: 12px;
			font-weight: bold;
		}

		.nav {
			display: flex;
			gap: 15px;
			margin-top: 20px;
			padding-top: 20px;
			border-top: 1px solid #eee;
		}

		.nav a {
			color: #667eea;
			text-decoration: none;
			font-size: 14px;
		}

		.nav a:hover {
			text-decoration: underline;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<h1>🛡️ MCP Proxy Dashboard</h1>
			<p style="color: #666;">Security-enhanced MCP server management</p>
		</header>

		<div class="stats-grid" id="stats">
			<div class="stat-card">
				<h3>Destructive Calls Blocked</h3>
				<div class="stat-value" id="blocked-count">0</div>
			</div>
			<div class="stat-card">
				<h3>Allowed Calls</h3>
				<div class="stat-value" id="allowed-count">0</div>
			</div>
			<div class="stat-card">
				<h3>Block Rate</h3>
				<div class="stat-value" id="block-rate">0%</div>
			</div>
			<div class="stat-card">
				<h3>Unique Blocked Tools</h3>
				<div class="stat-value" id="unique-blocked">0</div>
			</div>
		</div>

		<div class="servers-section">
			<h2>Configured Servers</h2>
			<div class="server-list" id="server-list">
				<p style="color: #999;">Loading servers...</p>
			</div>

			<div class="nav">
				<a href="/audit">📊 Audit Log</a>
				<a href="/settings">⚙️ Settings</a>
				<a href="https://github.com/yourusername/mcp-go-proxy" target="_blank">📖 Documentation</a>
			</div>
		</div>
	</div>

	<script>
		// Load stats
		fetch('/api/stats')
			.then(r => r.json())
			.then(data => {
				document.getElementById('blocked-count').textContent = data.blocked_calls_total;
				document.getElementById('allowed-count').textContent = data.allowed_calls_total;
				document.getElementById('block-rate').textContent = data.block_rate.toFixed(1) + '%';
				document.getElementById('unique-blocked').textContent = data.unique_blocked_tools;
			});

		// Load servers
		fetch('/api/servers')
			.then(r => r.json())
			.then(data => {
				const list = document.getElementById('server-list');
				list.innerHTML = '';

				if (data.servers.length === 0) {
					list.innerHTML = '<p style="color: #999;">No servers configured</p>';
					return;
				}

				data.servers.forEach(server => {
					const item = document.createElement('div');
					item.className = 'server-item';
					item.innerHTML = ` + "`" + `
						<div class="server-info">
							<h3>${server.name}</h3>
							<p>${server.transport} • ${server.url || server.command}</p>
						</div>
						<div class="server-status">Running</div>
					` + "`" + `;
					list.appendChild(item);
				});
			});

		// Refresh stats every 5 seconds
		setInterval(() => {
			fetch('/api/stats')
				.then(r => r.json())
				.then(data => {
					document.getElementById('blocked-count').textContent = data.blocked_calls_total;
					document.getElementById('allowed-count').textContent = data.allowed_calls_total;
					document.getElementById('block-rate').textContent = data.block_rate.toFixed(1) + '%';
				});
		}, 5000);
	</script>
</body>
</html>
`
}

func getAuditHTML() string {
	return `
<!DOCTYPE html>
<html>
<head>
	<title>Audit Log - MCP Proxy</title>
	<style>
		body {
			font-family: monospace;
			background: #1e1e1e;
			color: #d4d4d4;
			padding: 20px;
		}
		.container { max-width: 1200px; margin: 0 auto; }
		h1 { color: #667eea; }
		table { width: 100%; border-collapse: collapse; margin-top: 20px; }
		th, td { padding: 12px; text-align: left; border-bottom: 1px solid #444; }
		th { background: #333; color: #667eea; }
		a { color: #667eea; text-decoration: none; }
	</style>
</head>
<body>
	<div class="container">
		<h1>📊 Audit Log</h1>
		<p>Tool call audit trail and blocking events</p>
		<p style="color: #888; margin-top: 20px;">(Audit logging not yet implemented)</p>
		<p><a href="/">← Back to Dashboard</a></p>
	</div>
</body>
</html>
`
}

func getSettingsHTML() string {
	return `
<!DOCTYPE html>
<html>
<head>
	<title>Settings - MCP Proxy</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: #f5f5f5;
			padding: 20px;
		}
		.container { max-width: 800px; margin: 0 auto; background: white; padding: 30px; border-radius: 10px; }
		h1 { color: #333; }
		.setting { margin: 20px 0; padding: 15px; background: #f9f9f9; border-radius: 8px; }
		.setting h3 { color: #667eea; margin-bottom: 10px; }
		label { display: block; margin-bottom: 5px; color: #666; }
		select, input { padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
		button { background: #667eea; color: white; padding: 10px 20px; border: none; border-radius: 4px; cursor: pointer; }
		button:hover { background: #764ba2; }
		a { color: #667eea; text-decoration: none; }
	</style>
</head>
<body>
	<div class="container">
		<h1>⚙️ Settings</h1>

		<div class="setting">
			<h3>Security Policy</h3>
			<label for="policy">Policy Mode:</label>
			<select id="policy" onchange="updatePolicy()">
				<option value="strict">Strict</option>
				<option value="moderate" selected>Moderate</option>
				<option value="permissive">Permissive</option>
			</select>
		</div>

		<p><a href="/">← Back to Dashboard</a></p>
	</div>

	<script>
		// Load current policy
		fetch('/api/policy')
			.then(r => r.json())
			.then(data => {
				document.getElementById('policy').value = data.mode;
			});

		function updatePolicy() {
			const mode = document.getElementById('policy').value;
			fetch('/api/policy', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ mode })
			})
			.then(r => r.json())
			.then(data => alert('Policy updated: ' + data.mode));
		}
	</script>
</body>
</html>
`
}

func getBlocklistHTML() string {
	return `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Blocklist Management - MCP Proxy</title>
	<style>
		* {
			margin: 0;
			padding: 0;
			box-sizing: border-box;
		}

		html {
			scroll-behavior: smooth;
		}

		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: #f5f5f5;
			padding: 20px;
			line-height: 1.6;
			color: #333;
		}

		.container {
			max-width: 1200px;
			margin: 0 auto;
		}

		header {
			background: white;
			padding: 30px;
			border-radius: 10px;
			margin-bottom: 30px;
			box-shadow: 0 2px 8px rgba(0,0,0,0.1);
		}

		h1 {
			color: #2c3e50;
			margin-bottom: 8px;
			font-size: 28px;
		}

		.subtitle {
			color: #7f8c8d;
			margin-bottom: 20px;
			font-size: 14px;
		}

		.btn {
			background: #5568d3;
			color: white;
			padding: 12px 24px;
			border: none;
			border-radius: 6px;
			cursor: pointer;
			font-size: 14px;
			font-weight: 500;
			transition: background-color 0.2s ease, outline-offset 0.2s ease;
			min-height: 44px;
			min-width: 44px;
			display: inline-flex;
			align-items: center;
			justify-content: center;
		}

		.btn:hover {
			background: #4557c0;
		}

		.btn:focus {
			outline: 3px solid #5568d3;
			outline-offset: 2px;
		}

		.btn:active {
			background: #3d47a8;
		}

		.btn:disabled {
			background: #bdc3c7;
			cursor: not-allowed;
			opacity: 0.6;
		}

		.btn-danger {
			background: #e74c3c;
		}

		.btn-danger:hover {
			background: #c0392b;
		}

		.btn-danger:focus {
			outline: 3px solid #e74c3c;
			outline-offset: 2px;
		}

		.btn-secondary {
			background: #95a5a6;
		}

		.btn-secondary:hover {
			background: #7f8c8d;
		}

		.btn-secondary:focus {
			outline: 3px solid #95a5a6;
			outline-offset: 2px;
		}

		.button-group {
			display: flex;
			gap: 8px;
			flex-wrap: wrap;
		}

		table {
			width: 100%;
			border-collapse: collapse;
			background: white;
			border-radius: 10px;
			overflow: hidden;
			box-shadow: 0 2px 8px rgba(0,0,0,0.1);
			margin-bottom: 20px;
		}

		th, td {
			padding: 14px 15px;
			text-align: left;
			border-bottom: 1px solid #ecf0f1;
		}

		th {
			background: #34495e;
			color: white;
			font-weight: 600;
			font-size: 13px;
			text-transform: uppercase;
			letter-spacing: 0.5px;
		}

		tr:hover {
			background: #f9fafb;
		}

		td code {
			background: #ecf0f1;
			padding: 2px 6px;
			border-radius: 3px;
			font-family: 'Courier New', monospace;
			font-size: 13px;
		}

		.badge {
			display: inline-block;
			padding: 6px 10px;
			border-radius: 4px;
			font-size: 12px;
			font-weight: 600;
			white-space: nowrap;
		}

		.badge-block {
			background: #fadbd8;
			color: #78281f;
			border: 1px solid #f5b7b1;
		}

		.badge-allow {
			background: #d5f4e6;
			color: #0b5345;
			border: 1px solid #a9dfbf;
		}

		.badge-regex {
			background: #d6eaf8;
			color: #1a3a52;
			border: 1px solid #aed6f1;
		}

		.badge-semantic {
			background: #f4ecf7;
			color: #4a235a;
			border: 1px solid #d7bde2;
		}

		.badge-enabled {
			background: #d5f4e6;
			color: #0b5345;
		}

		.badge-disabled {
			background: #fadbd8;
			color: #78281f;
		}

		.modal {
			display: none;
			position: fixed;
			top: 0;
			left: 0;
			width: 100%;
			height: 100%;
			background: rgba(0, 0, 0, 0.5);
			z-index: 1000;
			align-items: center;
			justify-content: center;
			padding: 20px;
		}

		.modal.active {
			display: flex;
		}

		.modal-overlay {
			position: fixed;
			top: 0;
			left: 0;
			right: 0;
			bottom: 0;
			background: rgba(0, 0, 0, 0.5);
			z-index: 999;
		}

		.modal-content {
			background: white;
			padding: 30px;
			border-radius: 10px;
			max-width: 500px;
			width: 100%;
			max-height: 90vh;
			overflow-y: auto;
			position: relative;
			z-index: 1001;
			box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
		}

		.modal-header {
			margin-bottom: 24px;
			padding-bottom: 16px;
			border-bottom: 1px solid #ecf0f1;
		}

		.modal-header h2 {
			color: #2c3e50;
			font-size: 20px;
			margin: 0;
		}

		.modal-footer {
			display: flex;
			gap: 12px;
			margin-top: 24px;
			justify-content: flex-end;
		}

		.form-group {
			margin-bottom: 18px;
		}

		label {
			display: block;
			margin-bottom: 6px;
			color: #2c3e50;
			font-weight: 500;
			font-size: 14px;
		}

		input[type="text"],
		input[type="email"],
		input[type="password"],
		textarea,
		select {
			width: 100%;
			padding: 10px 12px;
			border: 1px solid #bdc3c7;
			border-radius: 6px;
			font-family: inherit;
			font-size: 14px;
			transition: border-color 0.2s ease, box-shadow 0.2s ease;
		}

		input[type="text"]:focus,
		input[type="email"]:focus,
		input[type="password"]:focus,
		textarea:focus,
		select:focus {
			border-color: #5568d3;
			box-shadow: 0 0 0 3px rgba(85, 104, 211, 0.1);
			outline: none;
		}

		input[type="text"]:disabled,
		input[type="email"]:disabled,
		input[type="password"]:disabled,
		textarea:disabled,
		select:disabled {
			background-color: #ecf0f1;
			cursor: not-allowed;
			opacity: 0.6;
		}

		textarea {
			resize: vertical;
			min-height: 80px;
		}

		.checkbox-group {
			display: flex;
			align-items: center;
			gap: 10px;
			margin-bottom: 12px;
		}

		.checkbox-label {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			cursor: pointer;
			user-select: none;
		}

		input[type="checkbox"] {
			width: 18px;
			height: 18px;
			cursor: pointer;
			accent-color: #5568d3;
		}

		input[type="checkbox"]:focus {
			outline: 2px solid #5568d3;
			outline-offset: 2px;
		}

		.permissions-grid {
			background: #f9fafb;
			padding: 12px;
			border-radius: 6px;
			border: 1px solid #ecf0f1;
		}

		.nav {
			margin-top: 30px;
			padding-top: 20px;
			border-top: 1px solid #ecf0f1;
			display: flex;
			gap: 20px;
			flex-wrap: wrap;
		}

		.nav a {
			color: #5568d3;
			text-decoration: none;
			font-weight: 500;
			transition: color 0.2s ease, outline-offset 0.2s ease;
			padding: 4px 2px;
		}

		.nav a:hover {
			color: #4557c0;
			text-decoration: underline;
		}

		.nav a:focus {
			outline: 2px solid #5568d3;
			outline-offset: 4px;
		}

		.alert {
			padding: 12px 16px;
			border-radius: 6px;
			margin-bottom: 16px;
			font-size: 14px;
			border-left: 4px solid;
		}

		.alert-error {
			background: #fadbd8;
			color: #78281f;
			border-left-color: #e74c3c;
		}

		.alert-success {
			background: #d5f4e6;
			color: #0b5345;
			border-left-color: #27ae60;
		}

		.skip-link {
			position: absolute;
			top: -40px;
			left: 0;
			background: #5568d3;
			color: white;
			padding: 8px 16px;
			text-decoration: none;
			border-radius: 0 0 6px 0;
			z-index: 9999;
		}

		.skip-link:focus {
			top: 0;
		}

		@media (max-width: 768px) {
			header {
				padding: 20px;
			}

			h1 {
				font-size: 22px;
			}

			.btn {
				padding: 10px 16px;
				font-size: 13px;
			}

			table {
				font-size: 13px;
			}

			th, td {
				padding: 10px 12px;
			}

			.modal-content {
				max-width: 95vw;
				padding: 20px;
			}

			.modal-footer {
				flex-direction: column;
			}

			.button-group {
				flex-direction: column;
			}

			.button-group .btn {
				width: 100%;
			}

			.nav {
				flex-direction: column;
				gap: 10px;
			}
		}

		@media (prefers-reduced-motion: reduce) {
			* {
				animation-duration: 0.01ms !important;
				animation-iteration-count: 1 !important;
				transition-duration: 0.01ms !important;
			}
		}
	</style>
</head>
<body>
	<a href="#main-content" class="skip-link">Skip to main content</a>

	<div class="container">
		<header>
			<h1>🔒 Blocklist Management</h1>
			<p class="subtitle">Manage tool blocklisting rules and permissions</p>
			<button class="btn" onclick="openModal()" aria-label="Create new blocklist rule">
				+ New Rule
			</button>
		</header>

		<div id="notification" role="status" aria-live="polite" aria-atomic="true"></div>

		<main id="main-content">
			<table role="table" aria-label="Blocklist rules">
				<thead>
					<tr>
						<th scope="col">Pattern</th>
						<th scope="col">Description</th>
						<th scope="col">Action</th>
						<th scope="col">Type</th>
						<th scope="col">Tools</th>
						<th scope="col">Status</th>
						<th scope="col">Actions</th>
					</tr>
				</thead>
				<tbody id="rules-table">
					<tr><td colspan="7" style="text-align: center; color: #999; padding: 30px;">Loading rules...</td></tr>
				</tbody>
			</table>
		</main>

		<nav class="nav" aria-label="Secondary navigation">
			<a href="/" title="Return to main dashboard">← Back to Dashboard</a>
			<a href="/audit" title="View system audit log">📊 Audit Log</a>
			<a href="/settings" title="Manage system settings">⚙️ Settings</a>
		</nav>
	</div>

	<!-- Modal for create/edit -->
	<div class="modal" id="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title">
		<div class="modal-overlay" onclick="closeModal()"></div>
		<div class="modal-content">
			<div class="modal-header">
				<h2 id="modal-title">Add New Rule</h2>
			</div>
			<form onsubmit="saveRule(event)">
				<div class="form-group">
					<label for="pattern">Pattern/Topic:</label>
					<input type="text" id="pattern" aria-required="true" required>
				</div>

				<div class="form-group">
					<label for="description">Description:</label>
					<textarea id="description" aria-describedby="description-help"></textarea>
					<small id="description-help" style="color: #7f8c8d; display: block; margin-top: 4px;">
						Briefly explain what this rule blocks or allows
					</small>
				</div>

				<div class="form-group">
					<label for="action">Action:</label>
					<select id="action" aria-required="true" onchange="updatePermissions()" required>
						<option value="">-- Select action --</option>
						<option value="block">Block</option>
						<option value="allow">Allow</option>
					</select>
				</div>

				<fieldset class="form-group">
					<legend style="font-weight: 500; color: #2c3e50; margin-bottom: 12px;">Match Type:</legend>
					<div class="checkbox-group">
						<input type="checkbox" id="is_regex">
						<label for="is_regex" class="checkbox-label">Regex pattern matching (fast)</label>
					</div>
					<div class="checkbox-group">
						<input type="checkbox" id="is_semantic" checked>
						<label for="is_semantic" class="checkbox-label">Semantic matching via Claude API (flexible)</label>
					</div>
				</fieldset>

				<div class="form-group">
					<label for="tools">Tools (comma-separated):</label>
					<input type="text" id="tools" placeholder="e.g., tool1, tool2" aria-describedby="tools-help">
					<small id="tools-help" style="color: #7f8c8d; display: block; margin-top: 4px;">
						Leave empty to apply rule to all tools
					</small>
				</div>

				<div class="form-group">
					<label>Permissions:</label>
					<div class="permissions-grid" id="permissions-grid">
						(Permissions configured automatically)
					</div>
				</div>

				<div class="modal-footer">
					<button type="submit" class="btn" aria-label="Save blocklist rule">
						Save Rule
					</button>
					<button type="button" class="btn btn-secondary" onclick="closeModal()" aria-label="Cancel and close dialog">
						Cancel
					</button>
				</div>
			</form>
		</div>
	</div>

	<script>
		let editingRuleId = null;

		// Notification system
		function showNotification(message, type = 'success') {
			const notif = document.getElementById('notification');
			notif.innerHTML = ` + "`" + `<div class="alert alert-${type}" role="alert">${message}</div>` + "`" + `;
			notif.focus();

			// Auto-hide after 5 seconds
			setTimeout(() => {
				notif.innerHTML = '';
			}, 5000);
		}

		function loadRules() {
			fetch('/api/blocklist')
				.then(r => r.json())
				.then(data => {
					const tbody = document.getElementById('rules-table');
					tbody.innerHTML = '';

					if (!data.rules || data.rules.length === 0) {
						tbody.innerHTML = '<tr><td colspan="7" style="text-align: center; color: #999; padding: 30px;">No rules configured</td></tr>';
						return;
					}

					data.rules.forEach(rule => {
						const row = document.createElement('tr');
						const typeBadges = [];
						if (rule.is_regex) typeBadges.push('<span class="badge badge-regex">Regex</span>');
						if (rule.is_semantic) typeBadges.push('<span class="badge badge-semantic">Semantic</span>');

						const statusBadgeClass = rule.enabled ? 'badge-enabled' : 'badge-disabled';
						const statusText = rule.enabled ? 'Enabled' : 'Disabled';

						row.innerHTML = ` + "`" + `
							<td><code>${escapeHtml(rule.pattern)}</code></td>
							<td>${escapeHtml(rule.description || '-')}</td>
							<td><span class="badge ${rule.action === 'block' ? 'badge-block' : 'badge-allow'}">${rule.action}</span></td>
							<td>${typeBadges.join(' ')}</td>
							<td>${escapeHtml(rule.tools || 'All')}</td>
							<td><span class="badge ${statusBadgeClass}">${statusText}</span></td>
							<td>
								<div class="button-group">
									<button class="btn" onclick="editRule(${rule.id})" aria-label="Edit rule: ${escapeHtml(rule.pattern)}">
										Edit
									</button>
									<button class="btn btn-danger" onclick="deleteRule(${rule.id})" aria-label="Delete rule: ${escapeHtml(rule.pattern)}">
										Delete
									</button>
								</div>
							</td>
						` + "`" + `;
						tbody.appendChild(row);
					});
				})
				.catch(err => {
					console.error('Failed to load rules:', err);
					showNotification('Error loading rules: ' + err.message, 'error');
				});
		}

		function openModal() {
			editingRuleId = null;
			document.getElementById('modal').classList.add('active');
			document.getElementById('pattern').value = '';
			document.getElementById('description').value = '';
			document.getElementById('action').value = '';
			document.getElementById('is_regex').checked = false;
			document.getElementById('is_semantic').checked = true;
			document.getElementById('tools').value = '';
			updatePermissions();

			// Focus trap: move focus to first input
			setTimeout(() => {
				document.getElementById('pattern').focus();
			}, 100);
		}

		function closeModal() {
			document.getElementById('modal').classList.remove('active');
			// Return focus to trigger button
			document.querySelector('[onclick="openModal()"]').focus();
		}

		// Close modal on Escape key
		document.addEventListener('keydown', (e) => {
			if (e.key === 'Escape' && document.getElementById('modal').classList.contains('active')) {
				closeModal();
			}
		});

		function editRule(ruleId) {
			fetch('/api/blocklist?id=' + ruleId)
				.then(r => r.json())
				.then(rule => {
					editingRuleId = ruleId;
					document.getElementById('pattern').value = rule.pattern;
					document.getElementById('description').value = rule.description || '';
					document.getElementById('action').value = rule.action;
					document.getElementById('is_regex').checked = rule.is_regex;
					document.getElementById('is_semantic').checked = rule.is_semantic;
					document.getElementById('tools').value = rule.tools || '';
					document.getElementById('modal-title').textContent = 'Edit Rule';
					updatePermissions();
					document.getElementById('modal').classList.add('active');
					document.getElementById('pattern').focus();
				})
				.catch(err => {
					showNotification('Error loading rule: ' + err.message, 'error');
				});
		}

		function saveRule(event) {
			event.preventDefault();

			const pattern = document.getElementById('pattern').value.trim();
			if (!pattern) {
				showNotification('Pattern/Topic is required', 'error');
				return;
			}

			const rule = {
				pattern: pattern,
				description: document.getElementById('description').value.trim(),
				action: document.getElementById('action').value,
				is_regex: document.getElementById('is_regex').checked,
				is_semantic: document.getElementById('is_semantic').checked,
				tools: document.getElementById('tools').value.trim()
			};

			const url = editingRuleId ? '/api/blocklist?id=' + editingRuleId : '/api/blocklist';
			const method = editingRuleId ? 'PUT' : 'POST';

			fetch(url, {
				method: method,
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(rule)
			})
			.then(r => r.json())
			.then(data => {
				closeModal();
				loadRules();
				showNotification('Rule ' + (editingRuleId ? 'updated' : 'created') + ' successfully', 'success');
			})
			.catch(err => {
				showNotification('Error saving rule: ' + err.message, 'error');
			});
		}

		function deleteRule(ruleId) {
			if (confirm('Are you sure you want to delete this rule? This action cannot be undone.')) {
				fetch('/api/blocklist?id=' + ruleId, { method: 'DELETE' })
					.then(r => r.json())
					.then(data => {
						loadRules();
						showNotification('Rule deleted successfully', 'success');
					})
					.catch(err => {
						showNotification('Error deleting rule: ' + err.message, 'error');
					});
			}
		}

		function updatePermissions() {
			// Placeholder for permissions UI
			document.getElementById('permissions-grid').innerHTML = '<em style="color: #7f8c8d;">Permissions are configured automatically based on action</em>';
		}

		function escapeHtml(text) {
			const div = document.createElement('div');
			div.textContent = text;
			return div.innerHTML;
		}

		// Load rules on page load
		loadRules();

		// Refresh rules every 30 seconds
		setInterval(loadRules, 30000);
	</script>
</body>
</html>
`
}
