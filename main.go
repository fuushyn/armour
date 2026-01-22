package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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
		case "serve":
			handleServeCommand()
			return
		case "version":
			fmt.Println("mcp-proxy v1.0.16")
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
	traceRecorder := proxy.NewTraceRecorder(200)

	// Get API key from environment (used for semantic blocklist matching)
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	// 2. Create stdio server (which initializes database and blocklist)
	stdioSrv, err := server.NewStdioServer(config, registry, statsTracker, policyManager, apiKey, traceRecorder)
	if err != nil {
		return fmt.Errorf("failed to create stdio server: %v", err)
	}
	defer stdioSrv.Close()

	// 2. Start Dashboard (Dual-Head)
	// Bind to localhost for security, hardcoded port for now (as per architecture)
	dashboardAddr := "127.0.0.1:13337"
	dashboardSrv := dashboard.NewDashboardServer(dashboardAddr, registry, config.ConfigPath, statsTracker, policyManager, stdioSrv.GetBlocklist(), stdioSrv.GetToolRegistry(), stdioSrv.GetDB(), logger, traceRecorder)

	if err := dashboardSrv.Start(); err != nil {
		log.Printf("Warning: failed to start dashboard: %v", err)
	} else {
		// Log to stderr so it doesn't interfere with stdio MCP traffic on stdout
		fmt.Fprintf(os.Stderr, "Dashboard started on http://%s\n", dashboardAddr)
	}
	// Keep dashboard running even after stdio exits
	defer func() {
		log.Printf("Shutting down dashboard")
		dashboardSrv.Stop()
	}()

	// 3. Start Stdio Server in a separate goroutine
	log.Printf("Stdio server starting (config: %s)", config.ConfigPath)

	// Run stdio server in goroutine so dashboard can keep running indefinitely
	go func() {
		if err := stdioSrv.Run(ctx); err != nil {
			log.Printf("Stdio server error: %v", err)
		}
	}()

	// Keep the process running so dashboard stays active
	// It will only exit when ctx is cancelled (on shutdown signal)
	<-ctx.Done()
	return ctx.Err()
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
	dashboardURL := "http://localhost:13337"

	// Check if dashboard is reachable
	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(dashboardURL + "/api/health")
	if err != nil {
		fmt.Println("Proxy not running. Start Claude Code to activate.")
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Proxy returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	// Open browser directly
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dashboardURL)
	case "linux":
		cmd = exec.Command("xdg-open", dashboardURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", dashboardURL)
	default:
		fmt.Printf("Dashboard: %s\n", dashboardURL)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Dashboard: %s\n", dashboardURL)
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

func handleServeCommand() {
	// Parse serve-specific flags
	port := 8084
	dbPath := ""
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	logLevel := "info"

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-port":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &port)
				i++
			}
		case "-db":
			if i+1 < len(os.Args) {
				dbPath = os.Args[i+1]
				i++
			}
		case "-api-key":
			if i+1 < len(os.Args) {
				apiKey = os.Args[i+1]
				i++
			}
		case "-log-level":
			if i+1 < len(os.Args) {
				logLevel = os.Args[i+1]
				i++
			}
		}
	}

	// Default DB path
	if dbPath == "" {
		homeDir, _ := os.UserHomeDir()
		dbPath = homeDir + "/.armour/rules.db"
	}

	config := server.RulesServerConfig{
		Port:     port,
		DBPath:   dbPath,
		APIKey:   apiKey,
		LogLevel: logLevel,
	}

	srv, err := server.NewRulesServer(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create rules server: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start rules server: %v\n", err)
		os.Exit(1)
	}

	// Write PID file
	srv.WritePIDFile()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down rules server...")
	srv.Stop()
}

func printHelp() {
	fmt.Print(`
MCP Go Proxy v1.0.16

USAGE:
  mcp-proxy [FLAGS] [COMMAND]

COMMANDS:
  detect        Detect existing MCP servers in standard locations
  up            Auto-discover and start MCP servers in current project
  serve         Start the rules server for instant policy enforcement
  backup        Backup MCP configurations
  recover       Restore MCP configurations from backup
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
