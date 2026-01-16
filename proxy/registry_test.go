package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerRegistry_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	config := `{
  "servers": [
    {"name": "greeter", "transport": "http", "url": "http://localhost:8082/greeter"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	registry, err := LoadServerRegistry(configPath)
	if err != nil {
		t.Fatalf("LoadServerRegistry failed: %v", err)
	}

	if len(registry.Servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(registry.Servers))
	}

	if registry.Servers[0].Name != "greeter" {
		t.Errorf("expected name 'greeter', got %s", registry.Servers[0].Name)
	}
}

func TestLoadServerRegistry_MissingConfig(t *testing.T) {
	_, err := LoadServerRegistry("")
	if err == nil {
		t.Fatal("expected error for missing config path")
	}
}

func TestLoadServerRegistry_MissingFile(t *testing.T) {
	_, err := LoadServerRegistry("/nonexistent/path/servers.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadServerRegistry_MultipleServers(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	config := `{
  "servers": [
    {"name": "greeter", "transport": "http", "url": "http://localhost:8082"},
    {"name": "hello", "transport": "stdio", "command": "/bin/echo"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	registry, err := LoadServerRegistry(configPath)
	if err != nil {
		t.Fatalf("LoadServerRegistry failed: %v", err)
	}

	if len(registry.Servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(registry.Servers))
	}
}

func TestLoadServerRegistry_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	config := `{
  "servers": [
    {"transport": "http", "url": "http://localhost:8082"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := LoadServerRegistry(configPath)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadServerRegistry_HTTPMissingURL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	config := `{
  "servers": [
    {"name": "bad", "transport": "http"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := LoadServerRegistry(configPath)
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestLoadServerRegistry_StdioMissingCommand(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "servers.json")

	config := `{
  "servers": [
    {"name": "bad", "transport": "stdio"}
  ]
}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := LoadServerRegistry(configPath)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestGetServer_SingleServer(t *testing.T) {
	registry := &ServerRegistry{
		Servers: []ServerEntry{
			{Name: "only", Transport: "http", URL: "http://localhost:8082"},
		},
	}

	server := registry.GetServer("")
	if server == nil {
		t.Fatal("expected server for empty id with single server")
	}
	if server.Name != "only" {
		t.Errorf("expected name 'only', got %s", server.Name)
	}
}

func TestGetServer_ByName(t *testing.T) {
	registry := &ServerRegistry{
		Servers: []ServerEntry{
			{Name: "first", Transport: "http", URL: "http://localhost:8082"},
			{Name: "second", Transport: "stdio", Command: "/bin/echo"},
		},
	}

	server := registry.GetServer("second")
	if server == nil {
		t.Fatal("expected server for 'second'")
	}
	if server.Name != "second" {
		t.Errorf("expected name 'second', got %s", server.Name)
	}
}

func TestGetServer_NotFound(t *testing.T) {
	registry := &ServerRegistry{
		Servers: []ServerEntry{
			{Name: "first", Transport: "http", URL: "http://localhost:8082"},
		},
	}

	server := registry.GetServer("missing")
	if server != nil {
		t.Fatal("expected nil for missing server")
	}
}
