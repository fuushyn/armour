package proxy

import (
	"testing"
	"time"
)

func TestElicitationBasic(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	em := NewElicitationManager()

	clientReq := NewInitRequest("TestClient", "1.0.0")
	clientReq.Params.Capabilities.Elicitation = &ElicitationCapability{Enabled: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Elicitation: &ElicitationCapability{Enabled: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	proxy.Initialize(clientReq, serverResp)

	em.AllowElicitationType("elicitation/confirm")

	if !em.IsElicitationAllowed("elicitation/confirm") {
		t.Errorf("expected elicitation type to be allowed")
	}
}

func TestElicitationBlocked(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	em := NewElicitationManager()

	clientReq := NewInitRequest("TestClient", "1.0.0")
	clientReq.Params.Capabilities.Elicitation = &ElicitationCapability{Enabled: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Elicitation: &ElicitationCapability{Enabled: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	proxy.Initialize(clientReq, serverResp)

	em.BlockElicitationType("elicitation/sensitive")

	if !em.IsElicitationBlocked("elicitation/sensitive") {
		t.Errorf("expected elicitation type to be blocked")
	}
}

func TestElicitationNotSupportedOnStdio(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	em := NewElicitationManager()

	clientReq := NewInitRequest("TestClient", "1.0.0")
	clientReq.Params.Capabilities.Elicitation = &ElicitationCapability{Enabled: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Elicitation: &ElicitationCapability{Enabled: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	proxy.Initialize(clientReq, serverResp)
	em.AllowElicitationType("elicitation/confirm")

	req := ElicitationRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "elicitation/confirm",
		Params:  make(map[string]interface{}),
	}

	err := em.ValidateElicitationRequest(req, proxy, "stdio")
	if err == nil {
		t.Errorf("expected error for elicitation on stdio")
	}
}

func TestAuditLogging(t *testing.T) {
	al := NewAuditLog()

	entry := AuditEntry{
		UserID:     "user123",
		AgentID:    "agent456",
		ServerID:   "server789",
		Method:     "sampling/createMessageResponse",
		Capability: "sampling",
		SessionID:  "session-abc",
		Transport:  "http",
		Timestamp:  time.Now().Unix(),
	}

	al.Log(entry)

	logs := al.GetLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].UserID != "user123" {
		t.Errorf("expected userID user123, got %s", logs[0].UserID)
	}
}

func TestAuditLoggingByUserID(t *testing.T) {
	al := NewAuditLog()

	al.Log(AuditEntry{UserID: "user1", ServerID: "server1", Method: "test"})
	al.Log(AuditEntry{UserID: "user1", ServerID: "server2", Method: "test"})
	al.Log(AuditEntry{UserID: "user2", ServerID: "server1", Method: "test"})

	logs := al.GetLogsByUserID("user1")
	if len(logs) != 2 {
		t.Errorf("expected 2 logs for user1, got %d", len(logs))
	}
}

func TestAuditLoggingByServerID(t *testing.T) {
	al := NewAuditLog()

	al.Log(AuditEntry{UserID: "user1", ServerID: "server1", Method: "test"})
	al.Log(AuditEntry{UserID: "user2", ServerID: "server1", Method: "test"})
	al.Log(AuditEntry{UserID: "user1", ServerID: "server2", Method: "test"})

	logs := al.GetLogsByServerID("server1")
	if len(logs) != 2 {
		t.Errorf("expected 2 logs for server1, got %d", len(logs))
	}
}

func TestOriginValidation(t *testing.T) {
	sm := NewSecurityManager()

	sm.AddAllowedOrigin("https://example.com")

	err := sm.ValidateOrigin("https://example.com")
	if err != nil {
		t.Errorf("expected origin to be allowed: %v", err)
	}

	err = sm.ValidateOrigin("https://evil.com")
	if err == nil {
		t.Errorf("expected origin to be rejected")
	}
}

func TestOriginRemoval(t *testing.T) {
	sm := NewSecurityManager()

	sm.AddAllowedOrigin("https://example.com")
	sm.RemoveAllowedOrigin("https://example.com")

	if sm.IsOriginAllowed("https://example.com") {
		t.Errorf("expected origin to be removed")
	}
}

func TestLocalHostOnly(t *testing.T) {
	sm := NewSecurityManager()

	if sm.IsLocalHostOnly() {
		t.Errorf("expected localHostOnly to be false initially")
	}

	sm.SetLocalHostOnly(true)
	if !sm.IsLocalHostOnly() {
		t.Errorf("expected localHostOnly to be true")
	}
}

func TestPayloadLogging(t *testing.T) {
	em := NewElicitationManager()

	if !em.logPayloads {
		t.Errorf("expected payload logging to be enabled initially")
	}

	em.EnablePayloadLogging(false)
	if em.logPayloads {
		t.Errorf("expected payload logging to be disabled")
	}
}

func TestSpecCompliance(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)

	clientReq := NewInitRequest("TestClient", "1.0.0")
	clientReq.Params.Capabilities.Sampling = &SamplingCapability{Tools: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Sampling: &SamplingCapability{Tools: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := proxy.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if proxy.GetProtocolVersion() != MCPProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", MCPProtocolVersion, proxy.GetProtocolVersion())
	}

	if !proxy.CanSample() {
		t.Errorf("expected sampling capability to be available")
	}
}

func TestCapabilityIntersectionRegressions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)

	clientReq := NewInitRequest("TestClient", "1.0.0")
	clientReq.Params.Capabilities.Sampling = &SamplingCapability{Tools: true}
	clientReq.Params.Capabilities.ListChanged = true

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Sampling:    &SamplingCapability{Tools: false},
			ListChanged: false,
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	proxy.Initialize(clientReq, serverResp)

	if proxy.CanSample() {
		t.Errorf("expected sampling to be disabled due to server capability")
	}

	if proxy.HasListChanged() {
		t.Errorf("expected listChanged to be disabled due to server capability")
	}
}
