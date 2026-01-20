package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	BackendID    string                 `json:"backendId,omitempty"`
	OriginalName string                 `json:"originalName,omitempty"` // Name without namespace prefix
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

// getDiscoveredToolsPath returns the path to the discovered tools file.
func getDiscoveredToolsPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".armour", "discovered-tools.json")
}

// SaveToFile persists the current tool registry to disk.
// This allows the dashboard to access discovered tools even when backends aren't connected.
func (tr *ToolRegistry) SaveToFile() error {
	tr.mu.RLock()
	tools := make([]RegisteredTool, 0, len(tr.tools))
	for _, tool := range tr.tools {
		tools = append(tools, *tool)
	}
	tr.mu.RUnlock()

	if len(tools) == 0 {
		return nil // Don't save empty registry
	}

	filePath := getDiscoveredToolsPath()
	if filePath == "" {
		return fmt.Errorf("could not determine home directory")
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"tools":     tools,
		"updatedAt": fmt.Sprintf("%d", os.Getpid()), // Include PID to help debug
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tools: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// LoadFromFile loads persisted tools from disk into the registry.
// This is used by the dashboard to show tools even when backends aren't connected.
func (tr *ToolRegistry) LoadFromFile() error {
	filePath := getDiscoveredToolsPath()
	if filePath == "" {
		return fmt.Errorf("could not determine home directory")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's fine
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	var stored struct {
		Tools []RegisteredTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("failed to parse tools file: %w", err)
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Only load if registry is empty (don't overwrite live data)
	if len(tr.tools) > 0 {
		return nil
	}

	for _, tool := range stored.Tools {
		toolCopy := tool
		tr.tools[tool.Name] = &toolCopy
		tr.backends[tool.Name] = tool.BackendID
	}

	return nil
}

// LoadDiscoveredTools is a standalone function to load discovered tools from file.
// Returns the tools list directly without needing a registry instance.
func LoadDiscoveredTools() ([]RegisteredTool, error) {
	filePath := getDiscoveredToolsPath()
	if filePath == "" {
		return nil, fmt.Errorf("could not determine home directory")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist yet
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var stored struct {
		Tools []RegisteredTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("failed to parse tools file: %w", err)
	}

	return stored.Tools, nil
}
