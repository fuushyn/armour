package proxy

import (
	"encoding/json"
	"fmt"
)

// ToBoolean converts an interface{} to bool, handling both bool and object types
func ToBoolean(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case map[string]interface{}:
		// If it's an object, treat it as truthy
		return len(val) > 0
	default:
		return false
	}
}

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
	Resources   *ResourcesCapability   `json:"resources,omitempty"`
	Prompts     *PromptsCapability     `json:"prompts,omitempty"`
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

// ResourcesCapability represents the resources capability
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability represents the prompts capability
type PromptsCapability struct {
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
				Tools: &ToolsCapability{
					ListChanged: false,
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
	// Both versions must be present
	if clientVersion == "" || serverVersion == "" {
		return fmt.Errorf("empty protocol version: client=%s, server=%s", clientVersion, serverVersion)
	}

	// Allow newer/older protocol versions to keep compatibility with clients that advance the version.
	return nil
}

func IntersectCapabilities(client, server Capabilities) Capabilities {
	// Intersect capabilities: only advertise what BOTH client and server support
	result := Capabilities{}

	// Sampling: both must support it
	if client.Sampling != nil && server.Sampling != nil {
		result.Sampling = &SamplingCapability{
			Tools: client.Sampling.Tools && server.Sampling.Tools,
		}
	}

	// Elicitation: both must support it
	if client.Elicitation != nil && server.Elicitation != nil {
		result.Elicitation = &ElicitationCapability{
			Enabled: client.Elicitation.Enabled && server.Elicitation.Enabled,
		}
	}

	// Tools: both must support it
	if client.Tools != nil && server.Tools != nil {
		result.Tools = &ToolsCapability{
			ListChanged: client.Tools.ListChanged && server.Tools.ListChanged,
		}
	}

	// ListChanged: both must support it
	result.ListChanged = ToBoolean(client.ListChanged) && ToBoolean(server.ListChanged)

	// Subscribe: both must support it
	result.Subscribe = ToBoolean(client.Subscribe) && ToBoolean(server.Subscribe)

	// Logging: both must support it
	clientLogging := ToBoolean(client.Logging)
	serverLogging := ToBoolean(server.Logging)
	if clientLogging && serverLogging {
		result.Logging = true
	}

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
