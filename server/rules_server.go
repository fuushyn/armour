package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RulesServer provides an HTTP API for instant rule checking
// Both the MCP proxy and PreToolUse hooks query this server
type RulesServer struct {
	db         *sql.DB
	apiKey     string
	httpServer *http.Server
	port       int
	logLevel   string
	mu         sync.RWMutex
}

// RulesServerConfig holds configuration for the rules server
type RulesServerConfig struct {
	Port     int
	DBPath   string
	APIKey   string // For semantic matching
	LogLevel string
}

// NewRulesServer creates a new rules server instance
func NewRulesServer(config RulesServerConfig) (*RulesServer, error) {
	// Open database
	db, err := sql.Open("sqlite", "file:"+config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize database schema
	if err := initRulesDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &RulesServer{
		db:       db,
		apiKey:   config.APIKey,
		port:     config.Port,
		logLevel: config.LogLevel,
	}, nil
}

// initRulesDB initializes the database schema for rules
func initRulesDB(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		pattern TEXT,
		topics TEXT,
		tools TEXT DEFAULT '*',
		scope TEXT DEFAULT 'all',
		action TEXT DEFAULT 'block',
		is_regex INTEGER DEFAULT 0,
		is_semantic INTEGER DEFAULT 0,
		block_all INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_rules_enabled ON rules(enabled);
	CREATE INDEX IF NOT EXISTS idx_rules_scope ON rules(scope);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add block_all column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE rules ADD COLUMN block_all INTEGER DEFAULT 0")

	return nil
}

// Start starts the HTTP server
func (rs *RulesServer) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/check", rs.handleCheck)
	mux.HandleFunc("/api/rules", rs.handleRules)
	mux.HandleFunc("/api/rules/", rs.handleRuleByID)
	mux.HandleFunc("/api/tools", rs.handleTools)
	mux.HandleFunc("/api/health", rs.handleHealth)

	// CORS middleware
	handler := rs.corsMiddleware(mux)

	rs.httpServer = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", rs.port),
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	listener, err := net.Listen("tcp", rs.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", rs.port, err)
	}

	rs.logInfo("Rules server starting on http://127.0.0.1:%d", rs.port)

	go func() {
		if err := rs.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			rs.logError("Rules server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the server
func (rs *RulesServer) Stop() error {
	if rs.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return rs.httpServer.Shutdown(ctx)
	}
	return nil
}

// Logging helpers
func (rs *RulesServer) logInfo(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
}

func (rs *RulesServer) logError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

func (rs *RulesServer) logDebug(format string, args ...interface{}) {
	if rs.logLevel == "debug" {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// corsMiddleware adds CORS headers
func (rs *RulesServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CheckRequest represents a rule check request
type CheckRequest struct {
	Tool    string `json:"tool"`
	Method  string `json:"method"`
	Content string `json:"content"`
	Scope   string `json:"scope"` // "native", "mcp", or "all"
}

// CheckResponse represents a rule check response
type CheckResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	RuleID  int    `json:"rule_id,omitempty"`
	// For hook compatibility
	Decision string `json:"decision"` // "allow" or "block"
}

// handleCheck handles rule check requests
// GET /api/check?tool=<name>&method=<method>&content=<text>&scope=<scope>
func (rs *RulesServer) handleCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	req := CheckRequest{
		Tool:    query.Get("tool"),
		Method:  query.Get("method"),
		Content: query.Get("content"),
		Scope:   query.Get("scope"),
	}

	if req.Scope == "" {
		req.Scope = "all"
	}

	// Get enabled rules
	rules, err := rs.getEnabledRules(req.Scope)
	if err != nil {
		rs.logError("Failed to get rules: %v", err)
		// Fail open - allow if we can't check
		json.NewEncoder(w).Encode(CheckResponse{
			Allowed:  true,
			Decision: "allow",
			Reason:   "rule check failed, defaulting to allow",
		})
		return
	}

	// Check each rule
	for _, rule := range rules {
		if !rs.ruleAppliesToTool(rule, req.Tool) {
			continue
		}

		matched := false

		// Block all - matches any call to the specified tool(s)
		if rule.BlockAll {
			matched = true
		}

		// Check pattern (regex)
		if !matched && rule.Pattern != "" && rule.IsRegex {
			if rs.matchesRegex(rule.Pattern, req.Content) {
				matched = true
			}
		}

		// Check pattern (literal)
		if !matched && rule.Pattern != "" && !rule.IsRegex && !rule.IsSemantic {
			if strings.Contains(strings.ToLower(req.Content), strings.ToLower(rule.Pattern)) {
				matched = true
			}
		}

		// Check semantic (if enabled and pattern didn't match)
		if !matched && rule.IsSemantic && rule.Topics != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			if rs.matchesSemantic(ctx, rule.Topics, req.Content) {
				matched = true
			}
			cancel()
		}

		if matched {
			if rule.Action == "block" {
				json.NewEncoder(w).Encode(CheckResponse{
					Allowed:  false,
					Decision: "block",
					Reason:   fmt.Sprintf("Blocked by rule: %s", rule.Name),
					RuleID:   rule.ID,
				})
				return
			}
			// action == "allow" means whitelist - explicitly allow
			json.NewEncoder(w).Encode(CheckResponse{
				Allowed:  true,
				Decision: "allow",
				Reason:   fmt.Sprintf("Allowed by rule: %s", rule.Name),
				RuleID:   rule.ID,
			})
			return
		}
	}

	// No rule matched - default allow
	json.NewEncoder(w).Encode(CheckResponse{
		Allowed:  true,
		Decision: "allow",
	})
}

// matchesRegex checks if content matches the regex pattern
func (rs *RulesServer) matchesRegex(pattern, content string) bool {
	matched, err := regexp.MatchString(pattern, content)
	if err != nil {
		rs.logError("Regex error for pattern %s: %v", pattern, err)
		return false
	}
	return matched
}

// matchesSemantic checks if content semantically matches the topics using Claude API
func (rs *RulesServer) matchesSemantic(ctx context.Context, topics, content string) bool {
	if rs.apiKey == "" || content == "" {
		return false
	}

	// Build prompt for semantic matching
	prompt := fmt.Sprintf(`Determine if the following content relates to any of these blocked topics: %s

Content to analyze:
%s

Respond with only "YES" if the content relates to any blocked topic, or "NO" if it doesn't.`, topics, content)

	reqBody := map[string]interface{}{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", strings.NewReader(string(jsonBody)))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", rs.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		rs.logError("Semantic check API error: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	if len(result.Content) > 0 {
		return strings.TrimSpace(strings.ToUpper(result.Content[0].Text)) == "YES"
	}

	return false
}

// Rule represents a rule in the database
type Rule struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Pattern    string    `json:"pattern,omitempty"`
	Topics     string    `json:"topics,omitempty"`
	Tools      string    `json:"tools"`
	Scope      string    `json:"scope"`
	Action     string    `json:"action"`
	IsRegex    bool      `json:"is_regex"`
	IsSemantic bool      `json:"is_semantic"`
	BlockAll   bool      `json:"block_all"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// getEnabledRules retrieves enabled rules filtered by scope
func (rs *RulesServer) getEnabledRules(scope string) ([]Rule, error) {
	query := `
		SELECT id, name, pattern, topics, tools, scope, action,
		       is_regex, is_semantic, COALESCE(block_all, 0), enabled, created_at, updated_at
		FROM rules
		WHERE enabled = 1 AND (scope = ? OR scope = 'all')
		ORDER BY id
	`

	rows, err := rs.db.Query(query, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var rule Rule
		var pattern, topics sql.NullString
		err := rows.Scan(
			&rule.ID, &rule.Name, &pattern, &topics, &rule.Tools,
			&rule.Scope, &rule.Action, &rule.IsRegex, &rule.IsSemantic,
			&rule.BlockAll, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rule.Pattern = pattern.String
		rule.Topics = topics.String
		rules = append(rules, rule)
	}
	return rules, nil
}

// ruleAppliesToTool checks if a rule applies to the given tool
func (rs *RulesServer) ruleAppliesToTool(rule Rule, toolName string) bool {
	tools := strings.TrimSpace(rule.Tools)
	if tools == "" || tools == "*" {
		return true
	}

	toolName = strings.ToLower(toolName)
	// Normalize tool name: replace __ and : with common separator for matching
	normalizedToolName := strings.ReplaceAll(toolName, "__", "_")
	normalizedToolName = strings.ReplaceAll(normalizedToolName, ":", "_")

	for _, t := range strings.Split(tools, ",") {
		t = strings.TrimSpace(strings.ToLower(t))
		// Normalize pattern too
		normalizedPattern := strings.ReplaceAll(t, "__", "_")
		normalizedPattern = strings.ReplaceAll(normalizedPattern, ":", "_")

		if t == toolName || normalizedPattern == normalizedToolName {
			return true
		}
		// Contains match (e.g., "*delete*" matches "file_delete_doc")
		if strings.HasPrefix(t, "*") && strings.HasSuffix(t, "*") && len(t) > 2 {
			keyword := t[1 : len(t)-1]
			if strings.Contains(toolName, keyword) || strings.Contains(normalizedToolName, keyword) {
				return true
			}
		}
		// Wildcard suffix match (e.g., "*delete" matches "file_delete")
		if strings.HasPrefix(t, "*") && !strings.HasSuffix(t, "*") {
			suffix := t[1:]
			if strings.HasSuffix(toolName, suffix) || strings.HasSuffix(normalizedToolName, suffix) {
				return true
			}
		}
		// Wildcard prefix match (e.g., "bash*" matches "bash_exec")
		if strings.HasSuffix(t, "*") && !strings.HasPrefix(t, "*") {
			prefix := t[:len(t)-1]
			if strings.HasPrefix(toolName, prefix) || strings.HasPrefix(normalizedToolName, prefix) {
				return true
			}
		}
		// Plain keyword contains match (no wildcards) - check if keyword appears in tool name
		if !strings.Contains(t, "*") {
			if strings.Contains(toolName, t) || strings.Contains(normalizedToolName, t) {
				return true
			}
		}
	}
	return false
}

// handleRules handles CRUD for rules collection
func (rs *RulesServer) handleRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		rs.listRules(w, r)
	case http.MethodPost:
		rs.createRule(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRuleByID handles operations on a specific rule
func (rs *RulesServer) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from path: /api/rules/123
	path := strings.TrimPrefix(r.URL.Path, "/api/rules/")
	id, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rs.getRule(w, id)
	case http.MethodPut:
		rs.updateRule(w, r, id)
	case http.MethodDelete:
		rs.deleteRule(w, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (rs *RulesServer) listRules(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT id, name, pattern, topics, tools, scope, action,
		       is_regex, is_semantic, COALESCE(block_all, 0), enabled, created_at, updated_at
		FROM rules ORDER BY id DESC
	`
	rows, err := rs.db.Query(query)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var rule Rule
		var pattern, topics sql.NullString
		err := rows.Scan(
			&rule.ID, &rule.Name, &pattern, &topics, &rule.Tools,
			&rule.Scope, &rule.Action, &rule.IsRegex, &rule.IsSemantic,
			&rule.BlockAll, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			continue
		}
		rule.Pattern = pattern.String
		rule.Topics = topics.String
		rules = append(rules, rule)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"rules": rules,
		"count": len(rules),
	})
}

func (rs *RulesServer) createRule(w http.ResponseWriter, r *http.Request) {
	var rule Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if rule.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if rule.Tools == "" {
		rule.Tools = "*"
	}
	if rule.Scope == "" {
		rule.Scope = "all"
	}
	if rule.Action == "" {
		rule.Action = "block"
	}

	result, err := rs.db.Exec(`
		INSERT INTO rules (name, pattern, topics, tools, scope, action, is_regex, is_semantic, block_all, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rule.Name, rule.Pattern, rule.Topics, rule.Tools, rule.Scope, rule.Action,
		rule.IsRegex, rule.IsSemantic, rule.BlockAll, true)

	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	rule.ID = int(id)
	rule.Enabled = true

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rule)
}

func (rs *RulesServer) getRule(w http.ResponseWriter, id int) {
	var rule Rule
	var pattern, topics sql.NullString
	err := rs.db.QueryRow(`
		SELECT id, name, pattern, topics, tools, scope, action,
		       is_regex, is_semantic, COALESCE(block_all, 0), enabled, created_at, updated_at
		FROM rules WHERE id = ?
	`, id).Scan(
		&rule.ID, &rule.Name, &pattern, &topics, &rule.Tools,
		&rule.Scope, &rule.Action, &rule.IsRegex, &rule.IsSemantic,
		&rule.BlockAll, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rule.Pattern = pattern.String
	rule.Topics = topics.String

	json.NewEncoder(w).Encode(rule)
}

func (rs *RulesServer) updateRule(w http.ResponseWriter, r *http.Request, id int) {
	var rule Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	_, err := rs.db.Exec(`
		UPDATE rules SET
			name = ?, pattern = ?, topics = ?, tools = ?, scope = ?,
			action = ?, is_regex = ?, is_semantic = ?, block_all = ?, enabled = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rule.Name, rule.Pattern, rule.Topics, rule.Tools, rule.Scope,
		rule.Action, rule.IsRegex, rule.IsSemantic, rule.BlockAll, rule.Enabled, id)

	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rule.ID = id
	json.NewEncoder(w).Encode(rule)
}

func (rs *RulesServer) deleteRule(w http.ResponseWriter, id int) {
	_, err := rs.db.Exec("DELETE FROM rules WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// handleTools returns list of known tools for the dropdown
func (rs *RulesServer) handleTools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Native Claude Code tools
	nativeTools := []map[string]string{
		{"name": "Bash", "scope": "native", "description": "Execute shell commands"},
		{"name": "Read", "scope": "native", "description": "Read file contents"},
		{"name": "Write", "scope": "native", "description": "Write/create files"},
		{"name": "Edit", "scope": "native", "description": "Edit existing files"},
		{"name": "Glob", "scope": "native", "description": "Find files by pattern"},
		{"name": "Grep", "scope": "native", "description": "Search file contents"},
		{"name": "WebFetch", "scope": "native", "description": "Fetch web content"},
		{"name": "WebSearch", "scope": "native", "description": "Search the web"},
		{"name": "Task", "scope": "native", "description": "Launch subagent"},
		{"name": "TodoWrite", "scope": "native", "description": "Manage todo list"},
		{"name": "NotebookEdit", "scope": "native", "description": "Edit Jupyter notebooks"},
	}

	// MCP tools from registry (if available)
	mcpTools := []map[string]string{}

	// Try to read from servers.json to get registered MCP servers
	homeDir, _ := os.UserHomeDir()
	serversPath := filepath.Join(homeDir, ".armour", "servers.json")
	if data, err := os.ReadFile(serversPath); err == nil {
		var config map[string]interface{}
		if json.Unmarshal(data, &config) == nil {
			if servers, ok := config["servers"].([]interface{}); ok {
				for _, s := range servers {
					if srv, ok := s.(map[string]interface{}); ok {
						if name, ok := srv["name"].(string); ok {
							mcpTools = append(mcpTools, map[string]string{
								"name":        name,
								"scope":       "mcp",
								"description": fmt.Sprintf("MCP server: %s", name),
							})
						}
					}
				}
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"native": nativeTools,
		"mcp":    mcpTools,
	})
}

// handleHealth returns server health status
func (rs *RulesServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"port":   rs.port,
	})
}

// GetPort returns the server port
func (rs *RulesServer) GetPort() int {
	return rs.port
}

// WritePIDFile writes the server's PID to a file for daemon management
func (rs *RulesServer) WritePIDFile() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pidPath := filepath.Join(homeDir, ".armour", "rules-server.pid")
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

// CheckRulesServer checks if the rules server is running
func CheckRulesServer(port int) bool {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// QueryRulesServer queries the rules server for a check
func QueryRulesServer(port int, tool, method, content, scope string) (*CheckResponse, error) {
	u := fmt.Sprintf("http://127.0.0.1:%d/api/check?tool=%s&method=%s&content=%s&scope=%s",
		port,
		url.QueryEscape(tool),
		url.QueryEscape(method),
		url.QueryEscape(content),
		url.QueryEscape(scope),
	)

	client := &http.Client{Timeout: 100 * time.Millisecond}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result CheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
