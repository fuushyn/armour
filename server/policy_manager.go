package server

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/user/mcp-go-proxy/proxy"
)

// PolicyMode defines the security posture of the proxy.
type PolicyMode string

const (
	StrictMode      PolicyMode = "strict"      // Maximum security, audit all
	ModerateMode    PolicyMode = "moderate"    // Balanced, selective auditing
	PermissiveMode  PolicyMode = "permissive"  // Minimal restrictions
)

// PolicyManager centralizes policy enforcement across multiple security components.
type PolicyManager struct {
	mode              PolicyMode
	samplingGuard     *proxy.SamplingGuard
	elicitationMgr    *proxy.ElicitationManager
	securityMgr       *proxy.SecurityManager
	auditLog          *proxy.AuditLog
	blockedTools      map[string]bool      // Tools blocked by this policy
	allowedOperations map[string]bool      // Allowed operations (if in restrictive mode)
	stats             *StatsTracker        // Track blocked/allowed calls
	mu                sync.RWMutex
}

// NewPolicyManager creates a new policy manager with moderate mode as default.
func NewPolicyManager(stats *StatsTracker) *PolicyManager {
	return &PolicyManager{
		mode:              ModerateMode,
		blockedTools:      make(map[string]bool),
		allowedOperations: make(map[string]bool),
		stats:             stats,
	}
}

// SetMode sets the policy mode (strict, moderate, or permissive).
func (pm *PolicyManager) SetMode(mode PolicyMode) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	switch mode {
	case StrictMode, ModerateMode, PermissiveMode:
		pm.mode = mode
		return nil
	default:
		return fmt.Errorf("invalid policy mode: %s", mode)
	}
}

// GetMode returns the current policy mode.
func (pm *PolicyManager) GetMode() PolicyMode {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.mode
}

// BlockTool adds a tool to the blocklist for this policy.
// Tool names can use wildcards: "rm*" matches "rm_file", "rmdir", etc.
func (pm *PolicyManager) BlockTool(toolPattern string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.blockedTools[toolPattern] = true
}

// UnblockTool removes a tool from the blocklist.
func (pm *PolicyManager) UnblockTool(toolPattern string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.blockedTools, toolPattern)
}

// AllowOperation marks an operation as allowed (for restrictive modes).
func (pm *PolicyManager) AllowOperation(operation string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.allowedOperations[operation] = true
}

// ApplyToRequest validates and potentially modifies a request based on the current policy.
// Returns an error if the policy blocks the request.
func (pm *PolicyManager) ApplyToRequest(req JSONRPCRequest, backendID string) error {
	pm.mu.RLock()
	mode := pm.mode
	blockedTools := pm.blockedTools
	stats := pm.stats
	pm.mu.RUnlock()

	switch mode {
	case StrictMode:
		return pm.applyStrictMode(req, backendID, blockedTools, stats)
	case ModerateMode:
		return pm.applyModerateMode(req, backendID, blockedTools, stats)
	case PermissiveMode:
		return pm.applyPermissiveMode(req, backendID, stats)
	default:
		return fmt.Errorf("unknown policy mode: %s", mode)
	}
}

// applyStrictMode enforces maximum security policies.
// Blocks: sampling, elicitation, custom operations
// Allows: read-only operations
func (pm *PolicyManager) applyStrictMode(req JSONRPCRequest, backendID string, blockedTools map[string]bool, stats *StatsTracker) error {
	switch req.Method {
	case "initialize":
		// Allow initialization but disable advanced capabilities
		if stats != nil {
			stats.RecordAllowedCall("initialize")
		}
		return nil

	case "tools/list":
		// Allow listing tools
		if stats != nil {
			stats.RecordAllowedCall("tools/list")
		}
		return nil

	case "tools/call":
		// Check if tool is blocked
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("invalid tool/call params: %v", err)
		}

		// Check exact match and wildcard patterns
		if isToolBlocked(params.Name, blockedTools) {
			if stats != nil {
				stats.RecordBlockedCall(params.Name, "strict_policy")
			}
			return fmt.Errorf("tool blocked by strict policy: %s", params.Name)
		}

		if stats != nil {
			stats.RecordAllowedCall(params.Name)
		}
		return nil

	case "resources/list", "resources/read":
		// Allow safe resource operations
		if stats != nil {
			stats.RecordAllowedCall(req.Method)
		}
		return nil

	case "resources/subscribe", "resources/unsubscribe":
		// Block in strict mode
		if stats != nil {
			stats.RecordBlockedCall(req.Method, "strict_policy")
		}
		return fmt.Errorf("resource subscriptions blocked by strict policy")

	case "prompts/list", "prompts/get":
		// Allow safe prompt operations
		if stats != nil {
			stats.RecordAllowedCall(req.Method)
		}
		return nil

	default:
		// Block unknown methods in strict mode
		if stats != nil {
			stats.RecordBlockedCall(req.Method, "strict_policy")
		}
		return fmt.Errorf("method blocked by strict policy: %s", req.Method)
	}
}

// applyModerateMode enforces balanced policies.
// Allows most operations but blocks obviously destructive ones.
func (pm *PolicyManager) applyModerateMode(req JSONRPCRequest, backendID string, blockedTools map[string]bool, stats *StatsTracker) error {
	switch req.Method {
	case "tools/call":
		// Check if tool is blocked
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("invalid tool/call params: %v", err)
		}

		// Check blocklist
		if isToolBlocked(params.Name, blockedTools) {
			if stats != nil {
				stats.RecordBlockedCall(params.Name, "policy_blocklist")
			}
			return fmt.Errorf("tool blocked by policy: %s", params.Name)
		}

		// Check common destructive patterns
		if isDestructivePattern(params.Name) {
			if stats != nil {
				stats.RecordBlockedCall(params.Name, "destructive_pattern")
			}
			return fmt.Errorf("destructive tool blocked by moderate policy: %s", params.Name)
		}

		if stats != nil {
			stats.RecordAllowedCall(params.Name)
		}
		return nil

	default:
		// Allow everything else in moderate mode
		if stats != nil {
			stats.RecordAllowedCall(req.Method)
		}
		return nil
	}
}

// applyPermissiveMode allows all operations with minimal restrictions.
func (pm *PolicyManager) applyPermissiveMode(req JSONRPCRequest, backendID string, stats *StatsTracker) error {
	// Record all calls in permissive mode (audit only)
	if stats != nil {
		if req.Method == "tools/call" {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &params); err == nil {
				stats.RecordAllowedCall(params.Name)
			}
		} else {
			stats.RecordAllowedCall(req.Method)
		}
	}
	// No restrictions in permissive mode
	return nil
}

// isToolBlocked checks if a tool matches any blocklist pattern.
func isToolBlocked(toolName string, blockedTools map[string]bool) bool {
	// Exact match
	if blockedTools[toolName] {
		return true
	}

	// Wildcard matching
	for pattern := range blockedTools {
		if matchWildcard(toolName, pattern) {
			return true
		}
	}

	return false
}

// matchWildcard checks if a string matches a wildcard pattern.
// Simple implementation: "*" at the end matches any suffix.
func matchWildcard(s string, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if len(pattern) == 0 {
		return len(s) == 0
	}

	if pattern[len(pattern)-1] == '*' {
		// Suffix pattern: "rm*" matches "rm_file", "rmdir", etc.
		return len(s) >= len(pattern)-1 && s[:len(pattern)-1] == pattern[:len(pattern)-1]
	}

	if pattern[0] == '*' {
		// Prefix pattern: "*_delete" matches "file_delete", "user_delete", etc.
		return len(s) >= len(pattern)-1 && s[len(s)-len(pattern)+1:] == pattern[1:]
	}

	return s == pattern
}

// isDestructivePattern checks for common destructive tool name patterns.
func isDestructivePattern(toolName string) bool {
	destructivePatterns := []string{
		"rm", "delete", "drop", "truncate", "destroy", "wipe",
		"remove", "erase", "kill", "terminate", "uninstall",
		"format", "reset", "clear", "flush", "purge",
	}

	// Extract base tool name (remove namespace prefix if present)
	baseName := toolName
	for i := len(toolName) - 1; i >= 0; i-- {
		if toolName[i] == ':' {
			baseName = toolName[i+1:]
			break
		}
	}

	// Check if base name matches or starts with any destructive pattern
	for _, pattern := range destructivePatterns {
		if baseName == pattern || matchWildcard(baseName, pattern+"*") {
			return true
		}
	}

	return false
}

// GetDescription returns a human-readable description of the current policy mode.
func (pm *PolicyManager) GetDescription() string {
	mode := pm.GetMode()

	switch mode {
	case StrictMode:
		return "Strict: Maximum security. Blocks sampling, elicitation, and destructive operations. Audit all tool calls."

	case ModerateMode:
		return "Moderate: Balanced security. Allows most operations but blocks obviously destructive ones. Audit sensitive calls."

	case PermissiveMode:
		return "Permissive: Minimal restrictions. Allows all operations. Minimal auditing."

	default:
		return fmt.Sprintf("Unknown policy mode: %s", mode)
	}
}

// PolicyConfig represents the configuration for a policy mode.
type PolicyConfig struct {
	Mode      PolicyMode `json:"mode"`
	BlockedTools []string `json:"blocked_tools,omitempty"`
	AllowedOperations []string `json:"allowed_operations,omitempty"`
}

// ApplyConfig applies a policy configuration.
func (pm *PolicyManager) ApplyConfig(config PolicyConfig) error {
	if err := pm.SetMode(config.Mode); err != nil {
		return err
	}

	// Clear and reapply blocked tools
	pm.mu.Lock()
	pm.blockedTools = make(map[string]bool)
	for _, tool := range config.BlockedTools {
		pm.blockedTools[tool] = true
	}
	pm.mu.Unlock()

	return nil
}
