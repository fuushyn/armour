package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DetectedServer represents an MCP server found during detection (server package version).
type DetectedServer struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`        // stdio, http, sse
	Command     string            `json:"command"`     // For stdio servers
	URL         string            `json:"url"`         // For http servers
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Source      string            `json:"source"`      // Where this was detected from
	Description string            `json:"description"`
}

// MigrationResult contains information about a migration operation.
type MigrationResult struct {
	ServersMigrated    int              `json:"servers_migrated"`
	BackupPath         string           `json:"backup_path"`
	ProxyConfigPath    string           `json:"proxy_config_path"`
	OriginalConfigPath string           `json:"original_config_path"`
	Servers            []DetectedServer `json:"servers"`
	Success            bool             `json:"success"`
	Message            string           `json:"message"`
}

// ConfigMigrator handles migration of MCP configs from Claude to the proxy.
type ConfigMigrator struct {
	homeDir                string
	proxyConfigPath        string
	claudeConfigPath       string
	claudeGlobalConfigPath string
}

// NewConfigMigrator creates a new config migrator.
func NewConfigMigrator() (*ConfigMigrator, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %v", err)
	}

	return &ConfigMigrator{
		homeDir:                homeDir,
		proxyConfigPath:        filepath.Join(homeDir, ".claude", "mcp-proxy", "servers.json"),
		claudeConfigPath:       filepath.Join(homeDir, ".claude", ".mcp.json"),
		claudeGlobalConfigPath: filepath.Join(homeDir, ".claude.json"),
	}, nil
}

// MigrateWithServers performs the migration with a provided list of servers.
// This is called after detection has been done separately.
func (cm *ConfigMigrator) MigrateWithServers(servers []DetectedServer, policyMode string) (*MigrationResult, error) {
	result := &MigrationResult{
		ProxyConfigPath:    cm.proxyConfigPath,
		OriginalConfigPath: cm.claudeConfigPath,
	}

	result.Servers = servers
	result.ServersMigrated = len(servers)

	// Ensure proxy config directory exists
	proxyConfigDir := filepath.Dir(cm.proxyConfigPath)
	if err := os.MkdirAll(proxyConfigDir, 0755); err != nil {
		result.Message = fmt.Sprintf("Failed to create config directory: %v", err)
		return result, err
	}

	// Convert DetectedServer to ServerEntry format for registry compatibility
	registryServers := make([]map[string]interface{}, len(servers))
	for i, srv := range servers {
		entry := map[string]interface{}{
			"name":      srv.Name,
			"transport": srv.Type,
		}
		if srv.URL != "" {
			entry["url"] = srv.URL
		}
		if srv.Command != "" {
			entry["command"] = srv.Command
		}
		if len(srv.Args) > 0 {
			entry["args"] = srv.Args
		}
		if len(srv.Env) > 0 {
			entry["env"] = srv.Env
		}
		registryServers[i] = entry
	}

	// Write proxy config with detected servers
	proxyConfig := map[string]interface{}{
		"servers": registryServers,
		"policy": map[string]string{
			"mode": policyMode,
		},
		"metadata": map[string]interface{}{
			"migrated_at": time.Now().Format(time.RFC3339),
			"version":     "1.0.16",
		},
	}

	configData, err := json.MarshalIndent(proxyConfig, "", "  ")
	if err != nil {
		result.Message = fmt.Sprintf("Failed to marshal proxy config: %v", err)
		return result, err
	}

	if err := os.WriteFile(cm.proxyConfigPath, configData, 0644); err != nil {
		result.Message = fmt.Sprintf("Failed to write proxy config: %v", err)
		return result, err
	}

	// Backup original Claude config if it exists
	if _, err := os.Stat(cm.claudeConfigPath); err == nil {
		backupPath := cm.claudeConfigPath + ".backup." + time.Now().Format("20060102-150405")
		if err := cm.backupFile(cm.claudeConfigPath, backupPath); err != nil {
			// Warning, not fatal
			fmt.Fprintf(os.Stderr, "Warning: failed to backup original config: %v\n", err)
		} else {
			result.BackupPath = backupPath
		}
	}

	// Update Claude config to point to proxy
	if err := cm.updateClaudeConfig(); err != nil {
		result.Message = fmt.Sprintf("Warning: could not update Claude config (manual step may be needed): %v", err)
		// Don't fail the entire migration
	}

	// Clean up ~/.claude.json to remove direct server references
	if err := cm.cleanClaudeGlobalConfig(); err != nil {
		result.Message = fmt.Sprintf("Warning: could not clean ~/.claude.json: %v", err)
	}

	result.Success = true
	result.Message = fmt.Sprintf("Successfully migrated %d server(s)", len(servers))

	return result, nil
}

// cleanClaudeGlobalConfig removes MCP servers from ~/.claude.json as they are now proxied.
func (cm *ConfigMigrator) cleanClaudeGlobalConfig() error {
	// 1. Check if file exists
	if _, err := os.Stat(cm.claudeGlobalConfigPath); os.IsNotExist(err) {
		return nil
	}

	// 2. Backup
	backupPath := cm.claudeGlobalConfigPath + ".backup." + time.Now().Format("20060102-150405")
	if err := cm.backupFile(cm.claudeGlobalConfigPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup .claude.json: %v", err)
	}

	// 3. Read and Parse
	data, err := os.ReadFile(cm.claudeGlobalConfigPath)
	if err != nil {
		return err
	}

	// Use generic map to preserve other fields
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// 4. Modify
	// Navigate to projects -> [each project] -> mcpServers
	if projects, ok := config["projects"].(map[string]interface{}); ok {
		for _, projVal := range projects {
			if project, ok := projVal.(map[string]interface{}); ok {
				if _, hasServers := project["mcpServers"]; hasServers {
					// Clear the mcpServers map
					project["mcpServers"] = make(map[string]interface{})
				}
			}
		}
	}

	// 5. Write back
	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(cm.claudeGlobalConfigPath, newData, 0644); err != nil {
		return err
	}

	return nil
}

// backupFile copies a file to a backup location.
func (cm *ConfigMigrator) backupFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err := io.Copy(dest, source); err != nil {
		return err
	}

	return dest.Sync()
}

// updateClaudeConfig updates the Claude config to use the proxy as the sole MCP entry.
func (cm *ConfigMigrator) updateClaudeConfig() error {
	// Read the main Claude config file
	mainConfigPath := filepath.Join(cm.homeDir, ".claude.json")
	data, err := os.ReadFile(mainConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read main Claude config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse Claude config: %v", err)
	}

	// Get current working directory to identify the project
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %v", err)
	}

	// Access projects map
	projects, ok := config["projects"].(map[string]interface{})
	if !ok {
		projects = make(map[string]interface{})
		config["projects"] = projects
	}

	// Get the current project config
	projectPath := cwd
	var projectConfig map[string]interface{}
	if proj, exists := projects[projectPath]; exists {
		projectConfig, _ = proj.(map[string]interface{})
	}
	if projectConfig == nil {
		projectConfig = make(map[string]interface{})
	}

	// Get the path to the proxy binary
	exePath, err := os.Executable()
	if err != nil {
		// Fallback to common location
		exePath = filepath.Join(cm.homeDir, ".claude-plugin", "mcp-proxy")
	}

	// Replace all MCP servers with the Sentinel proxy pointing to the registry
	projectConfig["mcpServers"] = map[string]interface{}{
		"sentinel": map[string]interface{}{
			"type":    "stdio",
			"command": exePath,
			"args":    []string{"-mode", "stdio", "-config", cm.proxyConfigPath},
			"env": map[string]string{
				"MCP_PROXY_CONFIG": cm.proxyConfigPath,
				"MCP_PROXY_POLICY": "moderate",
			},
		},
	}

	projects[projectPath] = projectConfig

	// Write the updated config back
	updatedData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %v", err)
	}

	if err := os.WriteFile(mainConfigPath, updatedData, 0600); err != nil {
		return fmt.Errorf("failed to write updated config to %s: %v", mainConfigPath, err)
	}

	return nil
}

// RollbackMigration restores the backed up config.
func (cm *ConfigMigrator) RollbackMigration(backupPath string) error {
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %v", err)
	}

	return cm.backupFile(backupPath, cm.claudeConfigPath)
}
