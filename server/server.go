package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/user/mcp-go-proxy/proxy"
)

type Config struct {
	ListenAddr     string
	LogLevel       string
	DBPath         string
	ConfigPath     string
	Mode           string
	AllowedOrigins []string
}

type Server struct {
	config       Config
	db           *sql.DB
	httpServer   *http.Server
	listener     net.Listener
	proxyManager *proxy.Proxy
	sessionMgr   *proxy.SessionManager
	resourceMgr  *proxy.ResourceManager
	oauth        *proxy.OAuth
	securityMgr  *proxy.SecurityManager
	auditLog     *proxy.AuditLog
	registry     *proxy.ServerRegistry
	forwarder    *proxy.Forwarder
	logger       *proxy.Logger
	mu           sync.RWMutex
	shutdown     chan struct{}
}

func NewServer(config Config) (*Server, error) {
	var db *sql.DB
	var err error

	if config.DBPath != "" {
		db, err = sql.Open("sqlite", "file:"+config.DBPath)
	} else {
		db, err = sql.Open("sqlite", "file:memdb?mode=memory&cache=shared")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	if err := initDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init database: %v", err)
	}

	registry, err := proxy.LoadServerRegistry(config.ConfigPath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load server registry: %v", err)
	}

	s := &Server{
		config:       config,
		db:           db,
		proxyManager: proxy.NewProxy(db),
		sessionMgr:   proxy.NewSessionManager(db),
		resourceMgr:  proxy.NewResourceManager(),
		oauth:        proxy.NewOAuth(),
		securityMgr:  proxy.NewSecurityManager(),
		auditLog:     proxy.NewAuditLog(),
		registry:     registry,
		forwarder:    proxy.NewForwarder(),
		logger:       proxy.NewLogger(config.LogLevel),
		shutdown:     make(chan struct{}),
	}

	for _, origin := range config.AllowedOrigins {
		s.securityMgr.AddAllowedOrigin(origin)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/mcp", s.handleMCP)

	s.httpServer = &http.Server{
		Addr:    config.ListenAddr,
		Handler: mux,
	}

	return s, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		if err := s.securityMgr.ValidateOrigin(origin); err != nil {
			s.logger.Warn("invalid origin: %s", origin)
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	if s.registry == nil {
		s.logger.Error("server registry not configured")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "server registry not configured"})
		return
	}

	sessionID := r.Header.Get(proxy.HeaderSessionID)
	if sessionID == "" {
		s.logger.Debug("missing session ID header")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing MCP-Session-Id header"})
		return
	}

	serverID := r.Header.Get(proxy.HeaderServerID)
	if serverID == "" {
		serverID = r.URL.Query().Get("server")
	}

	server := s.registry.GetServer(serverID)
	if server == nil {
		s.logger.Warn("server not found: %s", serverID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "server not found"})
		return
	}

	s.logger.Debug("incoming %s %s (session: %s, server: %s)", r.Method, r.RequestURI, sessionID, server.Name)

	switch r.Method {
	case http.MethodPost:
		s.handleMCPPost(w, r, server, sessionID)
	case http.MethodGet:
		s.handleMCPGet(w, r, server, sessionID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPPost(w http.ResponseWriter, r *http.Request, server *proxy.ServerEntry, sessionID string) {
	if server.Transport != "http" {
		s.logger.Error("POST on non-http server: %s", server.Transport)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only supported for http transport"})
		return
	}

	body, statusCode, err := s.forwarder.ForwardPOST(server.URL, sessionID, r.Body)
	if err != nil {
		s.logger.Error("failed to forward POST: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer body.Close()

	s.logger.Debug("upstream response: %d", statusCode)

	w.Header().Set(proxy.HeaderProtocolVersion, proxy.MCPProtocolVersion)
	w.Header().Set(proxy.HeaderSessionID, sessionID)
	if statusCode == http.StatusNoContent {
		w.WriteHeader(statusCode)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = io.Copy(w, body)
}

func (s *Server) handleMCPGet(w http.ResponseWriter, r *http.Request, server *proxy.ServerEntry, sessionID string) {
	if server.Transport != "http" {
		s.logger.Error("GET on non-http server: %s", server.Transport)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "GET only supported for http transport"})
		return
	}

	lastEventID := r.Header.Get("Last-Event-ID")
	resp, err := s.forwarder.ForwardGET(server.URL, sessionID, lastEventID)
	if err != nil {
		s.logger.Error("failed to forward GET: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	s.logger.Debug("upstream SSE stream: %d", resp.StatusCode)

	w.Header().Set(proxy.HeaderProtocolVersion, proxy.MCPProtocolVersion)
	w.Header().Set(proxy.HeaderSessionID, sessionID)
	if err := s.forwarder.PipeSSE(w, resp); err != nil {
		s.logger.Error("failed to pipe SSE: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer s.listener.Close()

	errChan := make(chan error, 1)

	go func() {
		s.logger.Info("server started on %s", s.listener.Addr().String())
		errChan <- s.httpServer.Serve(s.listener)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errChan:
		if err != http.ErrServerClosed {
			return err
		}
	}

	return nil
}

func (s *Server) GetListenAddr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.ListenAddr
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		s.db.Close()
	}

	return nil
}

func initDB(db *sql.DB) error {
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
		blocked INTEGER DEFAULT 0,
		block_reason TEXT,
		matched_pattern TEXT,
		denied_operation TEXT,
		rule_action TEXT,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS blocklist_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pattern TEXT NOT NULL,
		description TEXT,
		action TEXT CHECK(action IN ('block', 'allow', 'ask')) DEFAULT 'block',
		is_regex INTEGER DEFAULT 0,
		is_semantic INTEGER DEFAULT 1,
		tools TEXT DEFAULT '',
		perm_tools_call TEXT DEFAULT 'deny',
		perm_tools_list TEXT DEFAULT 'allow',
		perm_resources_read TEXT DEFAULT 'deny',
		perm_resources_list TEXT DEFAULT 'allow',
		perm_resources_subscribe TEXT DEFAULT 'deny',
		perm_prompts_get TEXT DEFAULT 'deny',
		perm_prompts_list TEXT DEFAULT 'allow',
		perm_sampling TEXT DEFAULT 'deny',
		enabled INTEGER DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_blocklist_enabled ON blocklist_rules(enabled);
	CREATE INDEX IF NOT EXISTS idx_blocklist_action ON blocklist_rules(action);
	`
	_, err := db.Exec(schema)
	return err
}

const shutdownTimeout = 30 * time.Second
