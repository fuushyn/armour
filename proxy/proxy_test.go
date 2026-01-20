package proxy

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitVersionSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	p := NewProxy(db)

	clientReq := NewInitRequest("TestClient", "1.0.16")
	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.16",
		},
		Capabilities: Capabilities{
			Sampling: &SamplingCapability{
				Tools: true,
			},
			Elicitation: &ElicitationCapability{
				Enabled: true,
			},
			ListChanged: true,
			Subscribe:   true,
			Logging:     false,
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := p.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if !p.IsInitialized() {
		t.Errorf("expected initialized=true, got false")
	}

	if p.GetProtocolVersion() != MCPProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", MCPProtocolVersion, p.GetProtocolVersion())
	}
}

func TestInitVersionMismatch(t *testing.T) {
	tests := []struct {
		name          string
		clientVersion string
		serverVersion string
		shouldFail    bool
	}{
		{"matching versions", MCPProtocolVersion, MCPProtocolVersion, false},
		{"client version mismatch", "2024-01-01", MCPProtocolVersion, true},
		{"server version mismatch", MCPProtocolVersion, "2024-01-01", true},
		{"both versions mismatch", "2024-01-01", "2024-06-01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()

			p := NewProxy(db)
			clientReq := InitRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
				Params: InitRequestParams{
					ClientInfo: ClientInfo{
						Name:    "TestClient",
						Version: "1.0.16",
					},
					Capabilities:    Capabilities{},
					ProtocolVersion: tt.clientVersion,
				},
			}

			serverResp := InitResponseResult{
				ServerInfo: ServerInfo{
					Name:    "TestServer",
					Version: "1.0.16",
				},
				Capabilities:    Capabilities{},
				ProtocolVersion: tt.serverVersion,
			}

			err := p.Initialize(clientReq, serverResp)
			if (err != nil) != tt.shouldFail {
				t.Errorf("initialize failed=%v, expected failed=%v: %v", err != nil, tt.shouldFail, err)
			}
		})
	}
}

func TestCapabilityIntersection(t *testing.T) {
	tests := []struct {
		name                string
		clientCaps          Capabilities
		serverCaps          Capabilities
		expectedSampling    *SamplingCapability
		expectedElicit      *ElicitationCapability
		expectedListChanged bool
		expectedSubscribe   bool
	}{
		{
			name: "both have all capabilities",
			clientCaps: Capabilities{
				Sampling:    &SamplingCapability{Tools: true},
				Elicitation: &ElicitationCapability{Enabled: true},
				ListChanged: true,
				Subscribe:   true,
			},
			serverCaps: Capabilities{
				Sampling:    &SamplingCapability{Tools: true},
				Elicitation: &ElicitationCapability{Enabled: true},
				ListChanged: true,
				Subscribe:   true,
			},
			expectedSampling:    &SamplingCapability{Tools: true},
			expectedElicit:      &ElicitationCapability{Enabled: true},
			expectedListChanged: true,
			expectedSubscribe:   true,
		},
		{
			name: "client missing sampling",
			clientCaps: Capabilities{
				Elicitation: &ElicitationCapability{Enabled: true},
			},
			serverCaps: Capabilities{
				Sampling:    &SamplingCapability{Tools: true},
				Elicitation: &ElicitationCapability{Enabled: true},
			},
			expectedSampling: nil,
			expectedElicit:   &ElicitationCapability{Enabled: true},
		},
		{
			name: "server missing elicitation",
			clientCaps: Capabilities{
				Elicitation: &ElicitationCapability{Enabled: true},
			},
			serverCaps:       Capabilities{},
			expectedSampling: nil,
			expectedElicit:   nil,
		},
		{
			name: "sampling.tools mismatch",
			clientCaps: Capabilities{
				Sampling: &SamplingCapability{Tools: true},
			},
			serverCaps: Capabilities{
				Sampling: &SamplingCapability{Tools: false},
			},
			expectedSampling: &SamplingCapability{Tools: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()

			p := NewProxy(db)
			clientReq := NewInitRequest("TestClient", "1.0.16")
			clientReq.Params.Capabilities = tt.clientCaps

			serverResp := InitResponseResult{
				ServerInfo: ServerInfo{
					Name:    "TestServer",
					Version: "1.0.16",
				},
				Capabilities:    tt.serverCaps,
				ProtocolVersion: MCPProtocolVersion,
			}

			if err := p.Initialize(clientReq, serverResp); err != nil {
				t.Fatalf("initialize failed: %v", err)
			}

			caps := p.GetIntersectedCapabilities()

			if (caps.Sampling == nil) != (tt.expectedSampling == nil) {
				t.Errorf("sampling nil mismatch: got %v, expected %v", caps.Sampling == nil, tt.expectedSampling == nil)
			}
			if caps.Sampling != nil && tt.expectedSampling != nil {
				if caps.Sampling.Tools != tt.expectedSampling.Tools {
					t.Errorf("sampling.tools: got %v, expected %v", caps.Sampling.Tools, tt.expectedSampling.Tools)
				}
			}

			if caps.ListChanged != tt.expectedListChanged {
				t.Errorf("listChanged: got %v, expected %v", caps.ListChanged, tt.expectedListChanged)
			}

			if caps.Subscribe != tt.expectedSubscribe {
				t.Errorf("subscribe: got %v, expected %v", caps.Subscribe, tt.expectedSubscribe)
			}
		})
	}
}

func TestCapabilityValidation(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		caps       Capabilities
		shouldFail bool
	}{
		{
			name:       "sampling allowed",
			capability: "sampling",
			caps: Capabilities{
				Sampling: &SamplingCapability{Tools: true},
			},
			shouldFail: false,
		},
		{
			name:       "sampling denied when not in intersection",
			capability: "sampling",
			caps:       Capabilities{},
			shouldFail: true,
		},
		{
			name:       "sampling.tools denied when tools=false",
			capability: "sampling.tools",
			caps: Capabilities{
				Sampling: &SamplingCapability{Tools: false},
			},
			shouldFail: true,
		},
		{
			name:       "elicitation allowed",
			capability: "elicitation",
			caps: Capabilities{
				Elicitation: &ElicitationCapability{Enabled: true},
			},
			shouldFail: false,
		},
		{
			name:       "resources.subscribe allowed",
			capability: "resources.subscribe",
			caps: Capabilities{
				Subscribe: true,
			},
			shouldFail: false,
		},
		{
			name:       "resources.listChanged allowed",
			capability: "resources.listChanged",
			caps: Capabilities{
				ListChanged: true,
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()

			p := NewProxy(db)
			clientReq := NewInitRequest("TestClient", "1.0.16")
			clientReq.Params.Capabilities = tt.caps

			serverResp := InitResponseResult{
				ServerInfo: ServerInfo{
					Name:    "TestServer",
					Version: "1.0.16",
				},
				Capabilities:    tt.caps,
				ProtocolVersion: MCPProtocolVersion,
			}

			if err := p.Initialize(clientReq, serverResp); err != nil {
				t.Fatalf("initialize failed: %v", err)
			}

			err := p.ValidateCapability(tt.capability)
			if (err != nil) != tt.shouldFail {
				t.Errorf("validate capability failed=%v, expected failed=%v: %v", err != nil, tt.shouldFail, err)
			}
		})
	}
}

func TestInitializedNotificationOnly(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	p := NewProxy(db)

	if p.IsInitialized() {
		t.Errorf("expected not initialized before Initialize call")
	}

	clientReq := NewInitRequest("TestClient", "1.0.16")
	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.16",
		},
		Capabilities:    Capabilities{},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := p.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if !p.IsInitialized() {
		t.Errorf("expected initialized after Initialize call")
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", "file:memdb?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}
