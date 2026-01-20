package proxy

import (
	"encoding/json"
	"testing"
)

func TestSamplingBlockedWhenCapabilityAbsent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	guard := NewSamplingGuard()

	clientReq := NewInitRequest("TestClient", "1.0.15")
	clientReq.Params.Capabilities.Sampling = nil

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.15",
		},
		Capabilities: Capabilities{
			Sampling: &SamplingCapability{
				Tools: false,
			},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := proxy.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	samplingReq := SamplingRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sampling/createMessageResponse",
		Params: SamplingParams{
			Messages: []Message{},
		},
	}

	err := guard.ValidateSamplingRequest(samplingReq, proxy, "stdio")
	if err == nil {
		t.Errorf("expected error when sampling capability absent, got nil")
	}
}

func TestSamplingAllowlistedByServer(t *testing.T) {
	guard := NewSamplingGuard()

	serverID := "trusted-server"
	guard.AllowServer(serverID)

	if !guard.IsServerAllowed(serverID) {
		t.Errorf("expected server to be allowed")
	}

	guard.DenyServer(serverID)
	if guard.IsServerAllowed(serverID) {
		t.Errorf("expected server to be denied after removal")
	}
}

func TestSamplingDisabledOnStdio(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	guard := NewSamplingGuard()
	guard.DisableOnTransport("stdio")

	clientReq := NewInitRequest("TestClient", "1.0.15")
	clientReq.Params.Capabilities.Sampling = &SamplingCapability{Tools: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.15",
		},
		Capabilities: Capabilities{
			Sampling: &SamplingCapability{Tools: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := proxy.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	samplingReq := SamplingRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sampling/createMessageResponse",
		Params: SamplingParams{
			Messages: []Message{},
		},
	}

	err := guard.ValidateSamplingRequest(samplingReq, proxy, "stdio")
	if err == nil || err.Error() != "sampling disabled on stdio transport" {
		t.Errorf("expected 'sampling disabled on stdio' error, got: %v", err)
	}
}

func TestToolUseToolResultBalance(t *testing.T) {
	tests := []struct {
		name      string
		messages  []Message
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid tool_use -> tool_result",
			messages: []Message{
				{
					Role:    "assistant",
					Content: json.RawMessage(`[{"type": "tool_use", "id": "tool-1", "name": "test", "input": {}}]`),
				},
				{
					Role:    "user",
					Content: json.RawMessage(`[{"type": "tool_result", "tool_use_id": "tool-1", "content": "result"}]`),
				},
			},
			shouldErr: false,
		},
		{
			name: "tool_result without matching tool_use",
			messages: []Message{
				{
					Role:    "user",
					Content: json.RawMessage(`[{"type": "tool_result", "tool_use_id": "unknown", "content": "result"}]`),
				},
			},
			shouldErr: true,
			errMsg:    "unknown tool_use_id",
		},
		{
			name: "mixed tool_result and text in user message after assistant",
			messages: []Message{
				{
					Role:    "assistant",
					Content: json.RawMessage(`[{"type": "tool_use", "id": "tool-1", "name": "test", "input": {}}]`),
				},
				{
					Role:    "user",
					Content: json.RawMessage(`[{"type": "tool_result", "tool_use_id": "tool-1", "content": "result"}, {"type": "text", "text": "extra"}]`),
				},
			},
			shouldErr: true,
			errMsg:    "cannot mix tool_result with other content",
		},
		{
			name: "only text in user message",
			messages: []Message{
				{
					Role:    "user",
					Content: json.RawMessage(`[{"type": "text", "text": "hello"}]`),
				},
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := NewSamplingGuard()
			err := guard.ValidateToolUseAndResultBalance(tt.messages)

			if (err != nil) != tt.shouldErr {
				t.Errorf("validate failed=%v, expected failed=%v: %v", err != nil, tt.shouldErr, err)
			}

			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

func TestSamplingToolsOnlyBothSidesDeclared(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tests := []struct {
		name            string
		clientSampling  *SamplingCapability
		serverSampling  *SamplingCapability
		expectedAllowed bool
	}{
		{
			name:            "both declare sampling.tools",
			clientSampling:  &SamplingCapability{Tools: true},
			serverSampling:  &SamplingCapability{Tools: true},
			expectedAllowed: true,
		},
		{
			name:            "client declares, server doesn't",
			clientSampling:  &SamplingCapability{Tools: true},
			serverSampling:  &SamplingCapability{Tools: false},
			expectedAllowed: false,
		},
		{
			name:            "server declares, client doesn't",
			clientSampling:  &SamplingCapability{Tools: false},
			serverSampling:  &SamplingCapability{Tools: true},
			expectedAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := NewProxy(db)

			clientReq := NewInitRequest("TestClient", "1.0.15")
			clientReq.Params.Capabilities.Sampling = tt.clientSampling

			serverResp := InitResponseResult{
				ServerInfo: ServerInfo{
					Name:    "TestServer",
					Version: "1.0.15",
				},
				Capabilities: Capabilities{
					Sampling: tt.serverSampling,
				},
				ProtocolVersion: MCPProtocolVersion,
			}

			if err := proxy.Initialize(clientReq, serverResp); err != nil {
				t.Fatalf("initialize failed: %v", err)
			}

			if proxy.CanSample() != tt.expectedAllowed {
				t.Errorf("expected CanSample()=%v, got %v", tt.expectedAllowed, proxy.CanSample())
			}
		})
	}
}

func TestSamplingRequestValidation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	proxy := NewProxy(db)
	guard := NewSamplingGuard()

	clientReq := NewInitRequest("TestClient", "1.0.15")
	clientReq.Params.Capabilities.Sampling = &SamplingCapability{Tools: true}

	serverResp := InitResponseResult{
		ServerInfo: ServerInfo{
			Name:    "TestServer",
			Version: "1.0.15",
		},
		Capabilities: Capabilities{
			Sampling: &SamplingCapability{Tools: true},
		},
		ProtocolVersion: MCPProtocolVersion,
	}

	if err := proxy.Initialize(clientReq, serverResp); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	samplingReq := SamplingRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sampling/createMessageResponse",
		Params: SamplingParams{
			Messages: []Message{
				{
					Role:    "user",
					Content: json.RawMessage(`[{"type": "text", "text": "hello"}]`),
				},
			},
		},
	}

	err := guard.ValidateSamplingRequest(samplingReq, proxy, "http")
	if err != nil {
		t.Errorf("expected valid sampling request to pass: %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
