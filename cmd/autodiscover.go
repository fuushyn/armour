package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectScanner scans a project directory for MCP server dependencies.
type ProjectScanner struct {
	rootDir string
	logger  interface{} // Would be proxy.Logger in real implementation
}

// DiscoveredProject represents a project with detected MCP servers.
type DiscoveredProject struct {
	Type        string                    // "npm", "docker", "python", "go"
	MCPServers  []DiscoveredMCPServer     // Detected servers
	ConfigFiles map[string]interface{}   // Found config files
}

// DiscoveredMCPServer represents a detected MCP server.
type DiscoveredMCPServer struct {
	Name        string            // Server identifier
	Package     string            // Package name (@modelcontextprotocol/server-github)
	Type        string            // "npm", "docker", "python", "go"
	Command     string            // Command to run
	Args        []string          // Arguments
	DockerImage string            // Docker image name (if docker)
	Source      string            // Where detected from
	Description string            // Human description
}

// NewProjectScanner creates a new project scanner.
func NewProjectScanner(rootDir string) *ProjectScanner {
	return &ProjectScanner{
		rootDir: rootDir,
	}
}

// Scan performs a comprehensive scan of the project directory.
func (ps *ProjectScanner) Scan() (*DiscoveredProject, error) {
	project := &DiscoveredProject{
		MCPServers:  make([]DiscoveredMCPServer, 0),
		ConfigFiles: make(map[string]interface{}),
	}

	// Check for NPM/Node.js MCP servers
	npmServers, err := ps.detectNPMMCPServers()
	if err == nil {
		project.MCPServers = append(project.MCPServers, npmServers...)
		if len(npmServers) > 0 {
			project.Type = "npm"
		}
	}

	// Check for Docker Compose MCP servers
	dockerServers, err := ps.detectDockerMCPServers()
	if err == nil {
		project.MCPServers = append(project.MCPServers, dockerServers...)
		if len(dockerServers) > 0 {
			project.Type = "docker"
		}
	}

	// Check for Python MCP servers
	pythonServers, err := ps.detectPythonMCPServers()
	if err == nil {
		project.MCPServers = append(project.MCPServers, pythonServers...)
		if len(pythonServers) > 0 {
			project.Type = "python"
		}
	}

	// Check for Go MCP servers
	goServers, err := ps.detectGoMCPServers()
	if err == nil {
		project.MCPServers = append(project.MCPServers, goServers...)
		if len(goServers) > 0 {
			project.Type = "go"
		}
	}

	if len(project.MCPServers) == 0 {
		return nil, fmt.Errorf("no MCP servers detected in project")
	}

	return project, nil
}

// detectNPMMCPServers scans package.json for MCP server dependencies.
func (ps *ProjectScanner) detectNPMMCPServers() ([]DiscoveredMCPServer, error) {
	packagePath := filepath.Join(ps.rootDir, "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return nil, fmt.Errorf("no package.json found")
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Scripts         map[string]string `json:"scripts"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse package.json: %v", err)
	}

	var servers []DiscoveredMCPServer
	mcpPackages := extractMCPPackages(pkg.Dependencies, pkg.DevDependencies)

	for _, pkgName := range mcpPackages {
		// Extract server name from package
		// @modelcontextprotocol/server-github -> github
		parts := strings.Split(pkgName, "-")
		serverName := parts[len(parts)-1]

		server := DiscoveredMCPServer{
			Name:        serverName,
			Package:     pkgName,
			Type:        "npm",
			Command:     "npx",
			Args:        []string{"-y", pkgName},
			Source:      "package.json",
			Description: fmt.Sprintf("NPM package: %s", pkgName),
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// detectDockerMCPServers scans docker-compose.yml for MCP services.
func (ps *ProjectScanner) detectDockerMCPServers() ([]DiscoveredMCPServer, error) {
	// Try multiple docker-compose file names
	dockerFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	var servers []DiscoveredMCPServer

	for _, fileName := range dockerFiles {
		filePath := filepath.Join(ps.rootDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue // Try next file
		}

		// Simple YAML parsing for docker-compose
		// In production, use a proper YAML parser
		content := string(data)
		lines := strings.Split(content, "\n")

		for i, line := range lines {
			line = strings.TrimSpace(line)

			// Look for image lines containing "mcp"
			if strings.HasPrefix(line, "image:") && strings.Contains(line, "mcp") {
				imageLine := strings.TrimPrefix(line, "image:")
				imageLine = strings.TrimSpace(imageLine)

				// Try to find service name (previous line with service name)
				serviceName := "docker-service"
				if i > 0 {
					prevLine := strings.TrimSpace(lines[i-1])
					if !strings.HasPrefix(prevLine, "-") && strings.HasSuffix(prevLine, ":") {
						serviceName = strings.TrimSuffix(prevLine, ":")
					}
				}

				server := DiscoveredMCPServer{
					Name:        serviceName,
					DockerImage: imageLine,
					Type:        "docker",
					Command:     "docker",
					Args:        []string{"compose", "up", serviceName},
					Source:      fileName,
					Description: fmt.Sprintf("Docker service: %s (%s)", serviceName, imageLine),
				}

				servers = append(servers, server)
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no docker-compose.yml with MCP services found")
	}

	return servers, nil
}

// detectPythonMCPServers scans pyproject.toml or requirements.txt for MCP packages.
func (ps *ProjectScanner) detectPythonMCPServers() ([]DiscoveredMCPServer, error) {
	var servers []DiscoveredMCPServer

	// Check pyproject.toml
	pyprojectPath := filepath.Join(ps.rootDir, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		content := string(data)
		if strings.Contains(content, "modelcontextprotocol") || strings.Contains(content, "mcp") {
			// Extract package names
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				if strings.Contains(line, "modelcontextprotocol") {
					// Parse package name
					pkgName := extractPythonPackageName(line)
					if pkgName != "" {
						serverName := strings.ReplaceAll(pkgName, "-", "_")
						server := DiscoveredMCPServer{
							Name:        serverName,
							Package:     pkgName,
							Type:        "python",
							Command:     "python",
							Args:        []string{"-m", serverName},
							Source:      "pyproject.toml",
							Description: fmt.Sprintf("Python package: %s", pkgName),
						}
						servers = append(servers, server)
					}
				}
			}
		}
	}

	// Check requirements.txt
	reqPath := filepath.Join(ps.rootDir, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		content := string(data)
		if strings.Contains(content, "modelcontextprotocol") || strings.Contains(content, "mcp") {
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "modelcontextprotocol") {
					pkgName := extractPythonPackageName(line)
					if pkgName != "" && !contains(servers, pkgName) {
						serverName := strings.ReplaceAll(pkgName, "-", "_")
						server := DiscoveredMCPServer{
							Name:        serverName,
							Package:     pkgName,
							Type:        "python",
							Command:     "python",
							Args:        []string{"-m", serverName},
							Source:      "requirements.txt",
							Description: fmt.Sprintf("Python package: %s", pkgName),
						}
						servers = append(servers, server)
					}
				}
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no Python MCP packages found")
	}

	return servers, nil
}

// detectGoMCPServers looks for Go MCP servers in go.mod.
func (ps *ProjectScanner) detectGoMCPServers() ([]DiscoveredMCPServer, error) {
	gomodPath := filepath.Join(ps.rootDir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return nil, fmt.Errorf("no go.mod found")
	}

	content := string(data)
	if !strings.Contains(content, "modelcontextprotocol") {
		return nil, fmt.Errorf("no MCP imports found in go.mod")
	}

	var servers []DiscoveredMCPServer

	// Look for internal Go MCP servers in cmd/ or main packages
	cmdPath := filepath.Join(ps.rootDir, "cmd")
	entries, err := os.ReadDir(cmdPath)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				// Assume each subdirectory in cmd/ could be a server
				server := DiscoveredMCPServer{
					Name:        entry.Name(),
					Type:        "go",
					Command:     "go",
					Args:        []string{"run", fmt.Sprintf("./cmd/%s", entry.Name())},
					Source:      "go.mod + cmd/",
					Description: fmt.Sprintf("Go MCP server: %s", entry.Name()),
				}
				servers = append(servers, server)
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no Go MCP servers detected")
	}

	return servers, nil
}

// Helper functions

// extractMCPPackages extracts @modelcontextprotocol/* packages from dependencies.
func extractMCPPackages(deps ...map[string]string) []string {
	var packages []string

	for _, depMap := range deps {
		for pkg := range depMap {
			if strings.Contains(pkg, "modelcontextprotocol") {
				packages = append(packages, pkg)
			}
		}
	}

	return packages
}

// extractPythonPackageName extracts the package name from a requirement line.
func extractPythonPackageName(line string) string {
	// Remove comments
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}

	line = strings.TrimSpace(line)

	// Remove version specifiers
	for _, sep := range []string{"==", ">=", "<=", ">", "<", "~=", "!="} {
		if idx := strings.Index(line, sep); idx >= 0 {
			line = line[:idx]
		}
	}

	return strings.TrimSpace(line)
}

// contains checks if a package name is already in the servers list.
func contains(servers []DiscoveredMCPServer, pkgName string) bool {
	for _, server := range servers {
		if server.Package == pkgName {
			return true
		}
	}
	return false
}

// FormatDiscoveryResults formats discovered servers for display.
func FormatDiscoveryResults(project *DiscoveredProject) string {
	output := fmt.Sprintf(`
%s MCP Servers Detected
═══════════════════════════════════════════

Project Type: %s
Found: %d MCP server(s)

`, strings.ToTitle(project.Type), project.Type, len(project.MCPServers))

	for i, server := range project.MCPServers {
		output += fmt.Sprintf(`
%d. %s
   Type: %s
   Command: %s %s
   Source: %s
   Description: %s

`, i+1, server.Name, server.Type, server.Command, strings.Join(server.Args, " "), server.Source, server.Description)
	}

	return output
}
