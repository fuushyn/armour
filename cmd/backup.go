package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupData stores all MCP configurations at a point in time
type BackupData struct {
	Timestamp       string                 `json:"timestamp"`
	GlobalMCP       map[string]interface{} `json:"global_mcp"`
	ClaudeProjects  map[string]interface{} `json:"claude_projects"`
	ArmourRegistry  map[string]interface{} `json:"armour_registry"`
	ProjectConfigs  map[string]interface{} `json:"project_configs"`
}

// CreateBackup saves all MCP configurations to ~/.armour/backup.json
func CreateBackup() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %v", err)
	}

	backupDir := filepath.Join(homeDir, ".armour")
	backupFile := filepath.Join(backupDir, "backup.json")

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	backup := &BackupData{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		GlobalMCP:      make(map[string]interface{}),
		ClaudeProjects: make(map[string]interface{}),
		ArmourRegistry: make(map[string]interface{}),
		ProjectConfigs: make(map[string]interface{}),
	}

	// Backup global ~/.claude/.mcp.json
	globalMCPPath := filepath.Join(homeDir, ".claude", ".mcp.json")
	if data, err := readJSON(globalMCPPath); err == nil {
		backup.GlobalMCP = data
	}

	// Backup ~/.claude.json (projects section)
	claudeConfigPath := filepath.Join(homeDir, ".claude.json")
	if data, err := readJSON(claudeConfigPath); err == nil {
		if projects, ok := data["projects"]; ok {
			if projectsMap, ok := projects.(map[string]interface{}); ok {
				backup.ClaudeProjects = projectsMap
			}
		}
	}

	// Backup ~/.armour/servers.json
	armourRegistryPath := filepath.Join(homeDir, ".armour", "servers.json")
	if data, err := readJSON(armourRegistryPath); err == nil {
		backup.ArmourRegistry = data
	}

	// Scan for project-level .mcp.json files
	projectDirs := scanProjectDirs(homeDir)
	for _, dir := range projectDirs {
		projectMCPPath := filepath.Join(dir, ".mcp.json")
		if data, err := readJSON(projectMCPPath); err == nil {
			backup.ProjectConfigs[dir] = data
		}
	}

	// Write backup file
	backupJSON, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup data: %v", err)
	}

	if err := os.WriteFile(backupFile, backupJSON, 0600); err != nil {
		return fmt.Errorf("failed to write backup file: %v", err)
	}

	fmt.Printf("✓ Backup created at %s\n", backupFile)
	return nil
}

// RestoreBackup restores all MCP configurations from ~/.armour/backup.json
func RestoreBackup() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %v", err)
	}

	backupFile := filepath.Join(homeDir, ".armour", "backup.json")

	// Check if backup exists
	if _, err := os.Stat(backupFile); err != nil {
		return fmt.Errorf("backup file not found at %s", backupFile)
	}

	// Read backup data
	backupJSON, err := os.ReadFile(backupFile)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %v", err)
	}

	var backup BackupData
	if err := json.Unmarshal(backupJSON, &backup); err != nil {
		return fmt.Errorf("failed to parse backup file: %v", err)
	}

	// Restore global ~/.claude/.mcp.json
	if len(backup.GlobalMCP) > 0 {
		globalMCPPath := filepath.Join(homeDir, ".claude", ".mcp.json")
		os.MkdirAll(filepath.Dir(globalMCPPath), 0755)
		if err := writeJSON(globalMCPPath, backup.GlobalMCP); err != nil {
			return fmt.Errorf("failed to restore global MCP config: %v", err)
		}
		fmt.Println("✓ Restored ~/.claude/.mcp.json")
	}

	// Restore ~/.claude.json (projects section)
	if len(backup.ClaudeProjects) > 0 {
		claudeConfigPath := filepath.Join(homeDir, ".claude.json")
		if data, err := readJSON(claudeConfigPath); err == nil {
			data["projects"] = backup.ClaudeProjects
			if err := writeJSON(claudeConfigPath, data); err != nil {
				return fmt.Errorf("failed to restore claude config: %v", err)
			}
			fmt.Println("✓ Restored ~/.claude.json projects")
		}
	}

	// Restore ~/.armour/servers.json
	if len(backup.ArmourRegistry) > 0 {
		armourRegistryPath := filepath.Join(homeDir, ".armour", "servers.json")
		os.MkdirAll(filepath.Dir(armourRegistryPath), 0755)
		if err := writeJSON(armourRegistryPath, backup.ArmourRegistry); err != nil {
			return fmt.Errorf("failed to restore armour registry: %v", err)
		}
		fmt.Println("✓ Restored ~/.armour/servers.json")
	}

	// Restore project-level .mcp.json files
	for projectDir, configData := range backup.ProjectConfigs {
		projectMCPPath := filepath.Join(projectDir, ".mcp.json")
		os.MkdirAll(filepath.Dir(projectMCPPath), 0755)
		if err := writeJSON(projectMCPPath, configData); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to restore %s: %v\n", projectMCPPath, err)
		} else {
			fmt.Printf("✓ Restored %s\n", projectMCPPath)
		}
	}

	fmt.Printf("\n✓ Restoration complete from backup (timestamp: %s)\n", backup.Timestamp)
	return nil
}

// Helper functions

func readJSON(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func writeJSON(path string, data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, jsonData, 0600)
}

func scanProjectDirs(homeDir string) []string {
	var projectDirs []string

	// Scan common project locations
	commonPaths := []string{
		homeDir,
		filepath.Join(homeDir, "dev"),
		filepath.Join(homeDir, "projects"),
		filepath.Join(homeDir, "src"),
	}

	for _, path := range commonPaths {
		if entries, err := os.ReadDir(path); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					projectDirs = append(projectDirs, filepath.Join(path, entry.Name()))
				}
			}
		}
	}

	return projectDirs
}
