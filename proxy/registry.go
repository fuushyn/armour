package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ServerEntry struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	URL       string            `json:"url,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type ServerRegistry struct {
	Servers []ServerEntry `json:"servers"`
}

func LoadServerRegistry(configPath string) (*ServerRegistry, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var registry ServerRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := validateRegistry(&registry); err != nil {
		return nil, err
	}

	return &registry, nil
}

func validateRegistry(registry *ServerRegistry) error {
	// Allow empty server list during initial setup - user will configure via /proxy-setup
	if len(registry.Servers) == 0 {
		return nil
	}

	for i, s := range registry.Servers {
		if s.Name == "" {
			return fmt.Errorf("server %d missing name", i)
		}
		if s.Transport == "" {
			return fmt.Errorf("server %s missing transport", s.Name)
		}
		if s.Transport == "http" && s.URL == "" {
			return fmt.Errorf("server %s (http) missing url", s.Name)
		}
		if s.Transport == "stdio" && s.Command == "" {
			return fmt.Errorf("server %s (stdio) missing command", s.Name)
		}
	}

	return nil
}

func SaveServerRegistry(registry *ServerRegistry, configPath string) error {
	if registry == nil {
		return fmt.Errorf("registry is nil")
	}
	if configPath == "" {
		return fmt.Errorf("config path required")
	}

	if err := validateRegistry(registry); err != nil {
		return err
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (r *ServerRegistry) GetServer(id string) *ServerEntry {
	if id == "" && len(r.Servers) == 1 {
		return &r.Servers[0]
	}

	for i := range r.Servers {
		if r.Servers[i].Name == id {
			return &r.Servers[i]
		}
	}

	return nil
}
