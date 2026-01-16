package server

import (
	"fmt"
	"sync"
)

// ToolRegistry manages tools from multiple backends with namespace separation.
// It prevents name collisions by prefixing tool names with their backend ID.
type ToolRegistry struct {
	tools    map[string]*RegisteredTool // Full tool name -> metadata
	backends map[string]string          // Full tool name -> backend ID
	mu       sync.RWMutex
}

// RegisteredTool represents a tool registered in the proxy with backend information.
type RegisteredTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"inputSchema,omitempty"`
	BackendID    string                 `json:"-"`
	OriginalName string                 `json:"-"` // Name without namespace prefix
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:    make(map[string]*RegisteredTool),
		backends: make(map[string]string),
	}
}

// RegisterBackendTools registers all tools from a backend, namespacing them to prevent collisions.
// Tools are prefixed with "{backendID}:" to ensure uniqueness.
func (tr *ToolRegistry) RegisterBackendTools(backendID string, tools []Tool) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if len(tools) == 0 {
		return nil // No tools to register
	}

	for _, tool := range tools {
		// Create namespaced tool name
		namespacedName := fmt.Sprintf("%s:%s", backendID, tool.Name)

		// Check for conflicts
		if existing, exists := tr.tools[namespacedName]; exists {
			// Tool already exists - this is a conflict
			return fmt.Errorf("tool name collision: %s already registered for backend %s", namespacedName, existing.BackendID)
		}

		// Register the tool
		tr.tools[namespacedName] = &RegisteredTool{
			Name:         namespacedName,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			BackendID:    backendID,
			OriginalName: tool.Name,
		}

		tr.backends[namespacedName] = backendID
	}

	return nil
}

// GetToolBackend returns the backend ID that owns a given tool name.
func (tr *ToolRegistry) GetToolBackend(toolName string) (string, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	backendID, exists := tr.backends[toolName]
	if !exists {
		return "", fmt.Errorf("tool not found: %s", toolName)
	}

	return backendID, nil
}

// GetTool returns the registered tool metadata for a given tool name.
func (tr *ToolRegistry) GetTool(toolName string) (*RegisteredTool, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tool, exists := tr.tools[toolName]
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}

	return tool, nil
}

// ListAllTools returns all registered tools, namespaced with their backend IDs.
func (tr *ToolRegistry) ListAllTools() []RegisteredTool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tools := make([]RegisteredTool, 0, len(tr.tools))
	for _, tool := range tr.tools {
		tools = append(tools, *tool)
	}

	return tools
}

// ToolCount returns the total number of registered tools.
func (tr *ToolRegistry) ToolCount() int {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	return len(tr.tools)
}

// ClearBackendTools removes all tools from a specific backend.
func (tr *ToolRegistry) ClearBackendTools(backendID string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Find all tools belonging to this backend
	toolsToRemove := []string{}
	for toolName, ownerBackendID := range tr.backends {
		if ownerBackendID == backendID {
			toolsToRemove = append(toolsToRemove, toolName)
		}
	}

	// Remove them
	for _, toolName := range toolsToRemove {
		delete(tr.tools, toolName)
		delete(tr.backends, toolName)
	}
}
