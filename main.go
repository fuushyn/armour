package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/user/mcp-go-proxy/cmd"
	"github.com/user/mcp-go-proxy/dashboard"
	"github.com/user/mcp-go-proxy/proxy"
	"github.com/user/mcp-go-proxy/server"
	_ "modernc.org/sqlite"
)

func main() {
	// Handle subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "detect":
			handleDetectCommand()
			return
		case "up":
			handleAutoDiscoverCommand()
			return
		case "migrate":
			handleMigrateCommand()
			return
		case "status":
			handleStatusCommand()
			return
		case "backup":
			handleBackupCommand()
			return
		case "recover":
			handleRecoverCommand()
			return
		case "version":
			fmt.Println("mcp-proxy v1.0.0")
			return
		case "help":
			printHelp()
			return
		}
	}

	args := cmd.ParseArgs()
	config := convertCLIArgsToServerConfig(args)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("received shutdown signal")
		cancel()
	}()

	// Route to appropriate mode
	if config.Mode == "stdio" {
		err := runStdioMode(ctx, config)
		if err != nil && err.Error() != "context canceled" {
			fmt.Fprintf(os.Stderr, "stdio mode error: %v\n", err)
			os.Exit(1)
		}
	} else if config.Mode == "http" {
		err := runHTTPMode(ctx, config)
		if err != nil && err.Error() != "context canceled" {
			fmt.Fprintf(os.Stderr, "http mode error: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", config.Mode)
		os.Exit(1)
	}

	log.Println("MCP Proxy shutdown complete")
}

func runHTTPMode(ctx context.Context, config server.Config) error {
	srv, err := server.NewServer(config)
	if err != nil {
		return fmt.Errorf("failed to create HTTP server: %v", err)
	}
	defer srv.Close()

	log.Printf("HTTP server starting on %s", config.ListenAddr)

	return srv.ListenAndServe(ctx)
}

func runStdioMode(ctx context.Context, config server.Config) error {
	// 1. Initialize shared components
	registry, err := proxy.LoadServerRegistry(config.ConfigPath)
	if err != nil {
		// Log warning but continue with empty registry (useful for initial setup)
		log.Printf("Note: starting with empty registry (config not found at %s)", config.ConfigPath)
		registry = &proxy.ServerRegistry{Servers: []proxy.ServerEntry{}}
	}

	statsTracker := server.NewStatsTracker()
	policyManager := server.NewPolicyManager(statsTracker)
	logger := proxy.NewLogger(config.LogLevel)

	// 2. Start Dashboard (Dual-Head)
	// Bind to localhost for security, hardcoded port for now (as per architecture)
	dashboardAddr := "127.0.0.1:13337"
	dashboardSrv := dashboard.NewDashboardServer(dashboardAddr, registry, statsTracker, policyManager, logger)

	if err := dashboardSrv.Start(); err != nil {
		log.Printf("Warning: failed to start dashboard: %v", err)
	} else {
		// Log to stderr so it doesn't interfere with stdio MCP traffic on stdout
		fmt.Fprintf(os.Stderr, "Dashboard started on http://%s\n", dashboardAddr)
	}
	defer dashboardSrv.Stop()

	// 3. Start Stdio Server
	stdioSrv, err := server.NewStdioServer(config, registry, statsTracker, policyManager)
	if err != nil {
		return fmt.Errorf("failed to create stdio server: %v", err)
	}

	log.Printf("Stdio server starting (config: %s)", config.ConfigPath)

	return stdioSrv.Run(ctx)
}

func handleDetectCommand() {
	detector, err := cmd.NewServerDetector()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	servers, err := detector.DetectAll()
	if err != nil && len(servers) == 0 {
		fmt.Fprintf(os.Stderr, "detection failed: %v\n", err)
		os.Exit(1)
	}

	// Output JSON for machine parsing
	if len(os.Args) > 2 && os.Args[2] == "--json" {
		data, _ := json.Marshal(servers)
		fmt.Println(string(data))
	} else {
		// Human-readable output
		fmt.Println(cmd.FormatDetectionResults(servers))
	}
}

func handleAutoDiscoverCommand() {
	scanner := cmd.NewProjectScanner(".")
	project, err := scanner.Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto-discovery failed: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 2 && os.Args[2] == "--json" {
		data, _ := json.Marshal(project)
		fmt.Println(string(data))
	} else {
		fmt.Println(cmd.FormatDiscoveryResults(project))
	}

	// TODO: Actually start the servers and create temp config
	fmt.Println("\n(Server startup not yet implemented)")
}

func handleMigrateCommand() {
	fmt.Println("🔍 Sentinel Proxy Migration Tool")
	fmt.Println("================================")

	// 1. Detect existing servers
	fmt.Print("• Detecting existing MCP servers... ")
	detector, err := cmd.NewServerDetector()
	if err != nil {
		fmt.Printf("Failed to create detector: %v\n", err)
		os.Exit(1)
	}

	servers, err := detector.DetectAll()
	if err != nil {
		fmt.Printf("Detection error: %v\n", err)
		// Continue if we found any servers
	}

	if len(servers) == 0 {
		fmt.Println("No existing servers found.")
		// Ask if user wants to install anyway? For now, just exit or proceed with empty
		fmt.Println("Proceeding with empty registry.")
	} else {
		fmt.Printf("Found %d servers.\n", len(servers))
	}

	// 2. Convert types (cmd.DetectedServer -> server.DetectedServer)
	var serverServers []server.DetectedServer
	for _, s := range servers {
		serverServers = append(serverServers, server.DetectedServer{
			Name:        s.Name,
			Type:        s.Type,
			Command:     s.Command,
			URL:         s.URL,
			Args:        s.Args,
			Env:         s.Env,
			Source:      s.Source,
			Description: s.Description,
		})
	}

	// 3. Perform Migration
	fmt.Print("• Migrating configuration... ")
	migrator, err := server.NewConfigMigrator()
	if err != nil {
		fmt.Printf("Failed to create migrator: %v\n", err)
		os.Exit(1)
	}

	// Default to moderate policy
	result, err := migrator.MigrateWithServers(serverServers, "moderate")
	if err != nil {
		fmt.Printf("Migration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done!")
	fmt.Println("\n✅ Migration Success!")
	fmt.Printf("- %d servers registered in %s\n", result.ServersMigrated, result.ProxyConfigPath)
	fmt.Printf("- Original config backed up to %s\n", result.BackupPath)
	fmt.Println("- Claude configuration updated to use Sentinel Proxy")
	fmt.Println("\n⚠️  Action Required: Restart Claude Code to apply changes.")
}

func handleStatusCommand() {
	// Check if dashboard is reachable
	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get("http://localhost:13337/api/health")
	if err != nil {
		fmt.Println("🔴 Sentinel Proxy is Inactive")
		fmt.Println("   (Could not connect to dashboard at localhost:13337)")
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("⚠️  Sentinel Proxy returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	// Fetch detailed stats
	statsResp, err := client.Get("http://localhost:13337/api/stats")
	if err == nil {
		defer statsResp.Body.Close()
		var stats map[string]interface{}
		json.NewDecoder(statsResp.Body).Decode(&stats)

		fmt.Println("🟢 Sentinel Proxy is Active")
		fmt.Println("   Status: Running")
		fmt.Printf("   Uptime: %.0fs\n", stats["uptime_seconds"])
		fmt.Printf("   Blocked Calls: %.0f\n", stats["blocked_calls_total"])
		fmt.Println("\n   Dashboard: http://localhost:13337")
	} else {
		fmt.Println("🟢 Sentinel Proxy is Active")
	}
}

func convertCLIArgsToServerConfig(args cmd.CLIArgs) server.Config {
	var origins []string
	if args.Origins != "" {
		origins = []string{args.Origins}
	}

	return server.Config{
		ListenAddr:     args.ListenAddr,
		Mode:           args.Mode,
		LogLevel:       args.LogLevel,
		DBPath:         args.DBPath,
		ConfigPath:     args.ConfigPath,
		AllowedOrigins: origins,
	}
}

func handleBackupCommand() {
	if err := cmd.CreateBackup(); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		os.Exit(1)
	}
}

func handleRecoverCommand() {
	if err := cmd.RestoreBackup(); err != nil {
		fmt.Fprintf(os.Stderr, "recovery failed: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`
MCP Go Proxy v1.0.0

USAGE:
  mcp-proxy [FLAGS] [COMMAND]

COMMANDS:
  detect        Detect existing MCP servers in standard locations
  up            Auto-discover and start MCP servers in current project
  version       Print version
  help          Print this help message

FLAGS:
  -mode STRING              Proxy mode: http or stdio (default: http)
  -config STRING            Path to servers.json configuration file
  -listen STRING            HTTP listen address (default: :8080)
  -log-level STRING         Log level: debug, info, warn, error (default: info)
  -db STRING                SQLite database path (default: in-memory)
  -origins STRING           Comma-separated allowed CORS origins
  -policy STRING            Default policy mode: strict, moderate, permissive

EXAMPLES:
  # Run as HTTP proxy on port 8080
  mcp-proxy -mode http -config servers.json

  # Run as stdio MCP server
  mcp-proxy -mode stdio -config servers.json

  # Detect existing MCP servers
  mcp-proxy detect

  # Auto-discover servers in current project
  mcp-proxy up

For more information, visit: https://github.com/yourusername/mcp-go-proxy
`)
}
