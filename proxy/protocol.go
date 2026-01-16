package proxy

import (
	"encoding/json"
	"fmt"
)

const (
	MCPProtocolVersion    = "2024-11-05"
	HeaderProtocolVersion = "MCP-Protocol-Version"
	HeaderSessionID       = "MCP-Session-Id"
	HeaderServerID        = "MCP-Server-Id"
)

type InitRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      interface{}       `json:"id,omitempty"`
	Method  string            `json:"method"`
	Params  InitRequestParams `json:"params"`
}

type InitRequestParams struct {
	ClientInfo      ClientInfo   `json:"clientInfo"`
	Capabilities    Capabilities `json:"capabilities"`
	ProtocolVersion string       `json:"protocolVersion"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      interface{}        `json:"id,omitempty"`
	Result  InitResponseResult `json:"result"`
}

type InitResponseResult struct {
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
	ProtocolVersion string       `json:"protocolVersion"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Sampling    *SamplingCapability    `json:"sampling,omitempty"`
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
	Tools       *ToolsCapability       `json:"tools,omitempty"`
	ListChanged bool                   `json:"listChanged,omitempty"`
	Subscribe   bool                   `json:"subscribe,omitempty"`
	Logging     interface{}            `json:"logging,omitempty"` // Can be bool or object
}

type SamplingCapability struct {
	Tools bool `json:"tools,omitempty"`
}

type ElicitationCapability struct {
	Enabled bool `json:"enabled,omitempty"`
}

// ToolsCapability represents the tools capability with list change support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

func NewInitRequest(clientName, clientVersion string) InitRequest {
	return InitRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: InitRequestParams{
			ClientInfo: ClientInfo{
				Name:    clientName,
				Version: clientVersion,
			},
			Capabilities: Capabilities{
				Sampling: &SamplingCapability{
					Tools: false,
				},
				ListChanged: false,
				Subscribe:   false,
				Logging:     false,
			},
			ProtocolVersion: MCPProtocolVersion,
		},
	}
}

func ValidateProtocolVersion(clientVersion, serverVersion string) error {
	// Per MCP spec: servers should accept the client's protocol version if it's reasonable
	// We accept any non-empty version string to maintain compatibility
	if clientVersion == "" || serverVersion == "" {
		return fmt.Errorf("empty protocol version: client=%s, server=%s", clientVersion, serverVersion)
	}
	// Accept the version negotiation - use client's version
	return nil
}

func IntersectCapabilities(client, server Capabilities) Capabilities {
	// Return server capabilities - don't intersect with client
	// The server announces what it supports; intersection is meaningless here
	result := server

	return result
}

type Notification struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

type InitializedNotification struct {
	JSONRPC string   `json:"jsonrpc"`
	Method  string   `json:"method"`
	Params  struct{} `json:"params"`
}

func NewInitializedNotification() InitializedNotification {
	return InitializedNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

const (
	ErrorCodeInvalidRequest         = -32600
	ErrorCodeMethodNotFound         = -32601
	ErrorCodeInvalidParams          = -32602
	ErrorCodeInternalError          = -32603
	ErrorCodeVersionMismatch        = -32000
	ErrorCodeCapabilityNotSupported = -32001
	ErrorCodeSessionNotFound        = -32002
)
