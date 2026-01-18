package dashboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/user/mcp-go-proxy/proxy"
	"github.com/user/mcp-go-proxy/server"
)

// Server provides a web-based dashboard for managing the MCP proxy.
type Server struct {
	listenAddr string
	httpServer *http.Server
	listener   net.Listener

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
	statsTracker *server.StatsTracker,
	policyManager *server.PolicyManager,
	blocklist *server.BlocklistMiddleware,
	db *sql.DB,
	logger *proxy.Logger,
) *Server {
	ds := &Server{
		listenAddr:    listenAddr,
		registry:      registry,
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

// handleServersAPI lists all configured servers.
func (ds *Server) handleServersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ds.mu.RLock()
	servers := ds.registry.Servers
	ds.mu.RUnlock()

	response := map[string]interface{}{
		"count":   len(servers),
		"servers": servers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

// handleBlocklistAPI manages blocklist rules (CRUD operations).
func (ds *Server) handleBlocklistAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract rule ID from query parameter if present
	ruleIDStr := r.URL.Query().Get("id")

	switch r.Method {
	case http.MethodGet:
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
			Pattern     string `json:"pattern"`
			Description string `json:"description,omitempty"`
			Action      string `json:"action"`
			IsRegex     bool   `json:"is_regex"`
			IsSemantic  bool   `json:"is_semantic"`
			Tools       string `json:"tools"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		rule := &server.BlocklistRule{
			Pattern:     req.Pattern,
			Description: req.Description,
			Action:      req.Action,
			IsRegex:     req.IsRegex,
			IsSemantic:  req.IsSemantic,
			Tools:       req.Tools,
			Permissions: server.DefaultPermissions(req.Action),
			Enabled:     true,
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
			Pattern     string `json:"pattern"`
			Description string `json:"description,omitempty"`
			Action      string `json:"action"`
			IsRegex     bool   `json:"is_regex"`
			IsSemantic  bool   `json:"is_semantic"`
			Tools       string `json:"tools"`
			Enabled     bool   `json:"enabled"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		rule := &server.BlocklistRule{
			ID:          ruleID,
			Pattern:     req.Pattern,
			Description: req.Description,
			Action:      req.Action,
			IsRegex:     req.IsRegex,
			IsSemantic:  req.IsSemantic,
			Tools:       req.Tools,
			Permissions: server.DefaultPermissions(req.Action),
			Enabled:     req.Enabled,
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
		"status": "ok",
		"version": "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UI Handlers

// handleDashboardUI serves the main dashboard page.
func (ds *Server) handleDashboardUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getDashboardHTML())
}

// handleAuditUI serves the audit log page.
func (ds *Server) handleAuditUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getAuditHTML())
}

// handleBlocklistUI serves the blocklist management page.
func (ds *Server) handleBlocklistUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getBlocklistHTML())
}

// handleSettingsUI serves the settings page.
func (ds *Server) handleSettingsUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, getSettingsHTML())
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
<html>
<head>
	<title>Blocklist Management - MCP Proxy</title>
	<style>
		* {
			margin: 0;
			padding: 0;
			box-sizing: border-box;
		}

		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: #f5f5f5;
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

		.btn {
			background: #667eea;
			color: white;
			padding: 10px 20px;
			border: none;
			border-radius: 6px;
			cursor: pointer;
			font-size: 14px;
		}

		.btn:hover {
			background: #764ba2;
		}

		.btn-danger {
			background: #dc3545;
		}

		.btn-danger:hover {
			background: #c82333;
		}

		table {
			width: 100%;
			border-collapse: collapse;
			background: white;
			border-radius: 10px;
			overflow: hidden;
			box-shadow: 0 4px 6px rgba(0,0,0,0.1);
		}

		th, td {
			padding: 12px 15px;
			text-align: left;
			border-bottom: 1px solid #eee;
		}

		th {
			background: #667eea;
			color: white;
			font-weight: 600;
		}

		tr:hover {
			background: #f9f9f9;
		}

		.badge {
			display: inline-block;
			padding: 4px 8px;
			border-radius: 4px;
			font-size: 12px;
			font-weight: 600;
		}

		.badge-block {
			background: #ffe0e0;
			color: #c82333;
		}

		.badge-allow {
			background: #e0ffe0;
			color: #155724;
		}

		.badge-regex {
			background: #e0e8ff;
			color: #004085;
		}

		.badge-semantic {
			background: #ffe0f5;
			color: #6f0047;
		}

		.modal {
			display: none;
			position: fixed;
			top: 0;
			left: 0;
			width: 100%;
			height: 100%;
			background: rgba(0,0,0,0.5);
			z-index: 1000;
		}

		.modal.active {
			display: flex;
			align-items: center;
			justify-content: center;
		}

		.modal-content {
			background: white;
			padding: 30px;
			border-radius: 10px;
			max-width: 500px;
			width: 90%;
			max-height: 80vh;
			overflow-y: auto;
		}

		.form-group {
			margin-bottom: 15px;
		}

		label {
			display: block;
			margin-bottom: 5px;
			color: #333;
			font-weight: 500;
		}

		input, textarea, select {
			width: 100%;
			padding: 8px 12px;
			border: 1px solid #ddd;
			border-radius: 6px;
			font-family: inherit;
			font-size: 14px;
		}

		textarea {
			resize: vertical;
			min-height: 60px;
		}

		.checkbox-group {
			display: flex;
			align-items: center;
			gap: 10px;
		}

		.nav {
			margin-top: 20px;
			padding-top: 20px;
			border-top: 1px solid #eee;
		}

		.nav a {
			color: #667eea;
			text-decoration: none;
			margin-right: 15px;
		}

		.nav a:hover {
			text-decoration: underline;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<h1>🔒 Blocklist Management</h1>
			<p style="color: #666;">Manage tool blocklisting rules</p>
			<button class="btn" onclick="openModal()">+ New Rule</button>
		</header>

		<table>
			<thead>
				<tr>
					<th>Pattern</th>
					<th>Description</th>
					<th>Action</th>
					<th>Type</th>
					<th>Tools</th>
					<th>Status</th>
					<th>Actions</th>
				</tr>
			</thead>
			<tbody id="rules-table">
				<tr><td colspan="7" style="text-align: center; color: #999;">Loading rules...</td></tr>
			</tbody>
		</table>

		<div class="nav">
			<a href="/">← Back to Dashboard</a>
			<a href="/audit">📊 Audit Log</a>
			<a href="/settings">⚙️ Settings</a>
		</div>
	</div>

	<!-- Modal for create/edit -->
	<div class="modal" id="modal">
		<div class="modal-content">
			<h2>Add New Rule</h2>
			<form onsubmit="saveRule(event)">
				<div class="form-group">
					<label for="pattern">Pattern/Topic:</label>
					<input type="text" id="pattern" required>
				</div>

				<div class="form-group">
					<label for="description">Description:</label>
					<textarea id="description"></textarea>
				</div>

				<div class="form-group">
					<label for="action">Action:</label>
					<select id="action" onchange="updatePermissions()">
						<option value="block">Block</option>
						<option value="allow">Allow</option>
					</select>
				</div>

				<div class="form-group">
					<label>Match Type:</label>
					<div>
						<label class="checkbox-group">
							<input type="checkbox" id="is_regex"> Regex
						</label>
						<label class="checkbox-group">
							<input type="checkbox" id="is_semantic" checked> Semantic
						</label>
					</div>
				</div>

				<div class="form-group">
					<label for="tools">Tools (comma-separated, leave empty for all):</label>
					<input type="text" id="tools" placeholder="tool1, tool2">
				</div>

				<div class="form-group">
					<label>Permissions:</label>
					<div id="permissions-grid"></div>
				</div>

				<button type="submit" class="btn">Save Rule</button>
				<button type="button" class="btn" onclick="closeModal()" style="background: #6c757d;">Cancel</button>
			</form>
		</div>
	</div>

	<script>
		let editingRuleId = null;

		function loadRules() {
			fetch('/api/blocklist')
				.then(r => r.json())
				.then(data => {
					const tbody = document.getElementById('rules-table');
					tbody.innerHTML = '';

					if (!data.rules || data.rules.length === 0) {
						tbody.innerHTML = '<tr><td colspan="7" style="text-align: center; color: #999;">No rules configured</td></tr>';
						return;
					}

					data.rules.forEach(rule => {
						const row = document.createElement('tr');
						const typeBadges = [];
						if (rule.is_regex) typeBadges.push('<span class="badge badge-regex">Regex</span>');
						if (rule.is_semantic) typeBadges.push('<span class="badge badge-semantic">Semantic</span>');

						row.innerHTML = ` + "`" + `
							<td><code>${rule.pattern}</code></td>
							<td>${rule.description || '-'}</td>
							<td><span class="badge ${rule.action === 'block' ? 'badge-block' : 'badge-allow'}">${rule.action}</span></td>
							<td>${typeBadges.join(' ')}</td>
							<td>${rule.tools || 'All'}</td>
							<td>${rule.enabled ? '✓ Enabled' : '✗ Disabled'}</td>
							<td>
								<button class="btn" onclick="editRule(${rule.id})">Edit</button>
								<button class="btn btn-danger" onclick="deleteRule(${rule.id})">Delete</button>
							</td>
						` + "`" + `;
						tbody.appendChild(row);
					});
				})
				.catch(err => {
					console.error('Failed to load rules:', err);
					document.getElementById('rules-table').innerHTML = '<tr><td colspan="7" style="text-align: center; color: red;">Error loading rules</td></tr>';
				});
		}

		function openModal() {
			editingRuleId = null;
			document.getElementById('pattern').value = '';
			document.getElementById('description').value = '';
			document.getElementById('action').value = 'block';
			document.getElementById('is_regex').checked = false;
			document.getElementById('is_semantic').checked = true;
			document.getElementById('tools').value = '';
			updatePermissions();
			document.getElementById('modal').classList.add('active');
		}

		function closeModal() {
			document.getElementById('modal').classList.remove('active');
		}

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
					updatePermissions();
					document.getElementById('modal').classList.add('active');
				});
		}

		function saveRule(event) {
			event.preventDefault();

			const rule = {
				pattern: document.getElementById('pattern').value,
				description: document.getElementById('description').value,
				action: document.getElementById('action').value,
				is_regex: document.getElementById('is_regex').checked,
				is_semantic: document.getElementById('is_semantic').checked,
				tools: document.getElementById('tools').value
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
				alert('Rule saved successfully');
			})
			.catch(err => alert('Error saving rule: ' + err));
		}

		function deleteRule(ruleId) {
			if (confirm('Delete this rule?')) {
				fetch('/api/blocklist?id=' + ruleId, { method: 'DELETE' })
					.then(r => r.json())
					.then(data => {
						loadRules();
						alert('Rule deleted successfully');
					})
					.catch(err => alert('Error deleting rule: ' + err));
			}
		}

		function updatePermissions() {
			// Placeholder for permissions UI
			document.getElementById('permissions-grid').innerHTML = '(Permissions configured automatically)';
		}

		// Load rules on page load
		loadRules();
	</script>
</body>
</html>
`
}
