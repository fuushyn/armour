package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectedServer represents an MCP server found during detection.
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

// ServerDetector scans the filesystem for existing MCP server configurations.
type ServerDetector struct {
	homeDir      string
	projectDirs  []string
	detectedDirs []string
}

// NewServerDetector creates a new server detector.
func NewServerDetector() (*ServerDetector, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %v", err)
	}

	return &ServerDetector{
		homeDir:      homeDir,
		projectDirs:  []string{},
		detectedDirs: []string{},
	}, nil
}

// AddProjectDir adds a project directory to search for MCP configurations.
func (sd *ServerDetector) AddProjectDir(dir string) {
	sd.projectDirs = append(sd.projectDirs, dir)
}

// DetectAll performs a comprehensive scan for all MCP servers across all known locations.
func (sd *ServerDetector) DetectAll() ([]DetectedServer, error) {
	var servers []DetectedServer
	var errors []error

	// Check Claude Code main config file (~/.claude.json)
	claudeConfigServers, err := sd.scanClaudeMainConfig(filepath.Join(sd.homeDir, ".claude.json"))
	if err == nil {
		servers = append(servers, claudeConfigServers...)
	}

	// Check user-level configuration
	userServers, err := sd.scanMCPConfig(filepath.Join(sd.homeDir, ".claude", ".mcp.json"))
	if err != nil {
		// Log but don't fail - user config might not exist
	}
	servers = append(servers, userServers...)

	// Check project directories
	cwd, err := os.Getwd()
	if err == nil {
		sd.AddProjectDir(cwd)

		// Check parent directories up to home
		for parent := cwd; parent != sd.homeDir && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			sd.projectDirs = append(sd.projectDirs, parent)
		}
	}

	for _, projectDir := range sd.projectDirs {
		// Check for .mcp.json in project
		projectServers, err := sd.scanMCPConfig(filepath.Join(projectDir, ".mcp.json"))
		if err == nil {
			servers = append(servers, projectServers...)
		}

		// Check for Claude Code project config
		claudeServers, err := sd.scanClaudeProjectConfig(projectDir)
		if err == nil {
			servers = append(servers, claudeServers...)
		}
	}

	// Check Claude Desktop config (platform-specific)
	claudeDesktopServers, err := sd.scanClaudeDesktopConfig()
	if err == nil {
		servers = append(servers, claudeDesktopServers...)
	}

	// Check Claude Code global config (~/.claude.json)
	globalServers, err := sd.scanClaudeGlobalConfig()
	if err == nil {
		servers = append(servers, globalServers...)
	}

	if len(errors) > 0 {
		fmt.Printf("Warning: encountered %d errors during detection\n", len(errors))
	}

	return servers, nil
}

// ScanClaudeGlobalConfig scans the global ~/.claude.json file
func (sd *ServerDetector) scanClaudeGlobalConfig() ([]DetectedServer, error) {
	configPath := filepath.Join(sd.homeDir, ".claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err // File doesn't exist or can't be read
	}

	var config struct {
		Projects map[string]struct {
			MCPServers map[string]interface{} `json:"mcpServers"`
		} `json:"projects"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse .claude.json: %v", err)
	}

	var servers []DetectedServer

	for projectPath, project := range config.Projects {
		for serverName, serverConfig := range project.MCPServers {
			// Parse server configuration
			configData, _ := json.Marshal(serverConfig)
			var entry struct {
				Type    string                 `json:"type"`
				Command string                 `json:"command"`
				Args    []string               `json:"args"`
				URL     string                 `json:"url"`
				Env     map[string]string      `json:"env"`
			}

			if err := json.Unmarshal(configData, &entry); err != nil {
				continue
			}

			// Skip the proxy itself
			if containsString(entry.Command, "mcp-proxy") || containsString(serverName, "sentinel") || containsString(serverName, "mcp-proxy") {
				continue
			}

			server := DetectedServer{
				Name:        serverName,
				Type:        entry.Type,
				Command:     entry.Command,
				Args:        entry.Args,
				URL:         entry.URL,
				Env:         entry.Env,
				Source:      fmt.Sprintf(".claude.json [%s]", filepath.Base(projectPath)),
				Description: fmt.Sprintf("Imported from project: %s", projectPath),
			}

			servers = append(servers, server)
		}
	}

	return servers, nil
}

// ScanMCPConfig scans an .mcp.json file for server definitions.
func (sd *ServerDetector) scanMCPConfig(configPath string) ([]DetectedServer, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %v", configPath, err)
	}

	var config struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", configPath, err)
	}

	var servers []DetectedServer

	for serverName, serverConfig := range config.MCPServers {
		// Parse server configuration
		configData, _ := json.Marshal(serverConfig)
		var entry struct {
			Type    string                 `json:"type"`
			Command string                 `json:"command"`
			Args    []string               `json:"args"`
			URL     string                 `json:"url"`
			Env     map[string]string      `json:"env"`
		}

		if err := json.Unmarshal(configData, &entry); err != nil {
			continue // Skip malformed entries
		}

		// Skip the proxy itself to prevent recursion
		if containsString(entry.Command, "mcp-proxy") || containsString(serverName, "sentinel") {
			continue
		}

		server := DetectedServer{
			Name:    serverName,
			Type:    entry.Type,
			Command: entry.Command,
			Args:    entry.Args,
			URL:     entry.URL,
			Env:     entry.Env,
			Source:  configPath,
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// scanClaudeMainConfig scans the main Claude Code config file (~/.claude.json)
// which contains per-project MCP server configurations.
func (sd *ServerDetector) scanClaudeMainConfig(configPath string) ([]DetectedServer, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %v", configPath, err)
	}

	var config struct {
		Projects map[string]struct {
			MCPServers map[string]interface{} `json:"mcpServers"`
		} `json:"projects"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", configPath, err)
	}

	var servers []DetectedServer

	// Extract servers from all projects
	for _, project := range config.Projects {
		for serverName, serverConfig := range project.MCPServers {
			// Parse server configuration
			configData, _ := json.Marshal(serverConfig)
			var entry struct {
				Type    string                 `json:"type"`
				Command string                 `json:"command"`
				Args    []string               `json:"args"`
				URL     string                 `json:"url"`
				Env     map[string]string      `json:"env"`
			}

			if err := json.Unmarshal(configData, &entry); err != nil {
				continue // Skip malformed entries
			}

			// Skip the proxy itself to prevent recursion
			if containsString(entry.Command, "mcp-proxy") || containsString(serverName, "sentinel") ||
				containsString(serverName, "mcp-go-proxy") {
				continue
			}

			server := DetectedServer{
				Name:    serverName,
				Type:    entry.Type,
				Command: entry.Command,
				Args:    entry.Args,
				URL:     entry.URL,
				Env:     entry.Env,
				Source:  configPath,
			}

			servers = append(servers, server)
		}
	}

	return servers, nil
}

// ScanClaudeProjectConfig scans Claude Code project configuration.
func (sd *ServerDetector) scanClaudeProjectConfig(projectDir string) ([]DetectedServer, error) {
	claudeDir := filepath.Join(projectDir, ".claude")
	mcsPath := filepath.Join(claudeDir, "mcp.json")

	return sd.scanMCPConfig(mcsPath)
}

// ScanClaudeDesktopConfig scans Claude Desktop's config file (macOS/Windows).
func (sd *ServerDetector) scanClaudeDesktopConfig() ([]DetectedServer, error) {
	var configPath string

	// Platform-specific paths for Claude Desktop config
	osType := os.Getenv("GOOS")
	if osType == "" {
		osType = "linux" // Fallback
	}

	switch osType {
	case "darwin":
		// macOS: ~/Library/Application Support/Claude/claude_desktop_config.json
		configPath = filepath.Join(sd.homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json")

	case "windows":
		// Windows: %APPDATA%\Claude\claude_desktop_config.json
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return nil, fmt.Errorf("APPDATA not set")
		}
		configPath = filepath.Join(appData, "Claude", "claude_desktop_config.json")

	default:
		// Linux and others: No official Claude Desktop path
		return nil, fmt.Errorf("Claude Desktop not supported on this platform")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("Claude Desktop config not found at %s", configPath)
	}

	var config struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Claude Desktop config: %v", err)
	}

	var servers []DetectedServer

	for serverName, serverConfig := range config.MCPServers {
		configData, _ := json.Marshal(serverConfig)
		var entry struct {
			Type    string                 `json:"type"`
			Command string                 `json:"command"`
			Args    []string               `json:"args"`
			URL     string                 `json:"url"`
			Env     map[string]string      `json:"env"`
		}

		if err := json.Unmarshal(configData, &entry); err != nil {
			continue
		}

		server := DetectedServer{
			Name:    serverName,
			Type:    entry.Type,
			Command: entry.Command,
			Args:    entry.Args,
			URL:     entry.URL,
			Env:     entry.Env,
			Source:  "Claude Desktop",
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// ConvertToProxyConfig converts detected servers to proxy registry format.
func ConvertDetectedServersToRegistry(servers []DetectedServer) map[string]interface{} {
	type ServerEntry struct {
		Name        string   `json:"name"`
		Transport   string   `json:"transport"`
		Command     string   `json:"command,omitempty"`
		Args        []string `json:"args,omitempty"`
		URL         string   `json:"url,omitempty"`
		Description string   `json:"description,omitempty"`
	}

	entries := make([]ServerEntry, 0, len(servers))

	for _, server := range servers {
		transport := server.Type
		if transport == "sse" || transport == "http" {
			transport = "http"
		}

		entry := ServerEntry{
			Name:        server.Name,
			Transport:   transport,
			Command:     server.Command,
			Args:        server.Args,
			URL:         server.URL,
			Description: server.Description,
		}

		entries = append(entries, entry)
	}

	return map[string]interface{}{
		"servers": entries,
	}
}

// FormatDetectionResults formats detected servers for display.
func FormatDetectionResults(servers []DetectedServer) string {
	if len(servers) == 0 {
		return "No MCP servers detected."
	}

	output := fmt.Sprintf("Found %d MCP server(s):\n\n", len(servers))

	for i, server := range servers {
		output += fmt.Sprintf("%d. %s (%s)\n", i+1, server.Name, server.Type)
		if server.Command != "" {
			output += fmt.Sprintf("   Command: %s\n", server.Command)
		}
		if server.URL != "" {
			output += fmt.Sprintf("   URL: %s\n", server.URL)
		}
		output += fmt.Sprintf("   Source: %s\n", server.Source)
		output += "\n"
	}

	return output
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
