package server

import (
	"time"
)

// Permission represents a permission level for blocklist rules
type Permission string

const (
	PermissionAllow   Permission = "allow"
	PermissionDeny    Permission = "deny"
	PermissionInherit Permission = "inherit"
)

// Permissions represents all available MCP permissions
type Permissions struct {
	ToolsCall         Permission `json:"tools_call"`
	ToolsList         Permission `json:"tools_list"`
	ResourcesRead     Permission `json:"resources_read"`
	ResourcesList     Permission `json:"resources_list"`
	ResourcesSubscribe Permission `json:"resources_subscribe"`
	PromptsGet        Permission `json:"prompts_get"`
	PromptsList       Permission `json:"prompts_list"`
	Sampling          Permission `json:"sampling"`
}

// BlocklistRule represents a single blocklist rule stored in the database
type BlocklistRule struct {
	ID          int64       `json:"id"`
	Pattern     string      `json:"pattern"`
	Description string      `json:"description,omitempty"`
	Action      string      `json:"action"` // block, allow
	IsRegex     bool        `json:"is_regex"`
	IsSemantic  bool        `json:"is_semantic"`
	Tools       string      `json:"tools"` // comma-separated tool names
	Permissions Permissions `json:"permissions"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// BlocklistCheckResult represents the result of a blocklist check
type BlocklistCheckResult struct {
	Allowed         bool          `json:"allowed"`
	DeniedOperation string        `json:"denied_operation,omitempty"` // e.g., "tools_call"
	MatchedRule     *BlocklistRule `json:"matched_rule,omitempty"`
	Error           *MCPError      `json:"error,omitempty"`
}

// MCPError represents an MCP error response
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DefaultPermissions returns the default permission set for new block rules
func DefaultPermissions(action string) Permissions {
	if action == "allow" {
		// Allow all operations for allow rules
		return Permissions{
			ToolsCall:         PermissionAllow,
			ToolsList:         PermissionAllow,
			ResourcesRead:     PermissionAllow,
			ResourcesList:     PermissionAllow,
			ResourcesSubscribe: PermissionAllow,
			PromptsGet:        PermissionAllow,
			PromptsList:       PermissionAllow,
			Sampling:          PermissionAllow,
		}
	}

	// Block all operations except list operations for block rules
	return Permissions{
		ToolsCall:         PermissionDeny,
		ToolsList:         PermissionAllow,
		ResourcesRead:     PermissionDeny,
		ResourcesList:     PermissionAllow,
		ResourcesSubscribe: PermissionDeny,
		PromptsGet:        PermissionDeny,
		PromptsList:       PermissionAllow,
		Sampling:          PermissionDeny,
	}
}
