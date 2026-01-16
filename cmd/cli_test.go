package cmd

import (
	"testing"
)

func TestCLIArgsDefaults(t *testing.T) {
	args := ParseArgsWithArgs([]string{})

	if args.ListenAddr != ":8080" {
		t.Errorf("expected default listen :8080, got %s", args.ListenAddr)
	}

	if args.Mode != "http" {
		t.Errorf("expected default mode http, got %s", args.Mode)
	}

	if args.LogLevel != "info" {
		t.Errorf("expected default log level info, got %s", args.LogLevel)
	}

	if args.DBPath != "" {
		t.Errorf("expected empty DBPath by default, got %s", args.DBPath)
	}
}

func TestCLIArgsCustom(t *testing.T) {
	args := ParseArgsWithArgs([]string{
		"-listen", ":9000",
		"-mode", "stdio",
		"-log-level", "debug",
		"-db", "/tmp/mcp.db",
		"-origins", "https://example.com",
	})

	if args.ListenAddr != ":9000" {
		t.Errorf("expected listen :9000, got %s", args.ListenAddr)
	}

	if args.Mode != "stdio" {
		t.Errorf("expected mode stdio, got %s", args.Mode)
	}

	if args.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", args.LogLevel)
	}

	if args.DBPath != "/tmp/mcp.db" {
		t.Errorf("expected DBPath /tmp/mcp.db, got %s", args.DBPath)
	}

	if args.Origins != "https://example.com" {
		t.Errorf("expected origins https://example.com, got %s", args.Origins)
	}
}

func TestCLIArgsToServerConfig(t *testing.T) {
	args := CLIArgs{
		ListenAddr: ":8080",
		Mode:       "http",
		LogLevel:   "info",
		DBPath:     "",
		Origins:    "https://example.com",
	}

	config := CLIArgsToServerConfig(args)

	if config.ListenAddr != ":8080" {
		t.Errorf("expected ListenAddr :8080, got %s", config.ListenAddr)
	}

	if config.Mode != "http" {
		t.Errorf("expected Mode http, got %s", config.Mode)
	}

	if len(config.AllowedOrigins) != 1 || config.AllowedOrigins[0] != "https://example.com" {
		t.Errorf("expected one allowed origin, got %v", config.AllowedOrigins)
	}
}

func TestCLIArgsToServerConfigNoOrigins(t *testing.T) {
	args := CLIArgs{
		ListenAddr: ":8080",
		Mode:       "http",
		Origins:    "",
	}

	config := CLIArgsToServerConfig(args)

	if len(config.AllowedOrigins) != 0 {
		t.Errorf("expected no origins, got %v", config.AllowedOrigins)
	}
}
