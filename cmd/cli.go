package cmd

import (
	"flag"
	"os"
)

type CLIArgs struct {
	ListenAddr string
	Mode       string
	LogLevel   string
	DBPath     string
	ConfigPath string
	Origins    string
}

func ParseArgs() CLIArgs {
	return ParseArgsWithArgs(os.Args[1:])
}

func ParseArgsWithArgs(args []string) CLIArgs {
	cliArgs := CLIArgs{}

	fs := flag.NewFlagSet("mcp-proxy", flag.ContinueOnError)
	fs.StringVar(&cliArgs.ListenAddr, "listen", ":8080", "HTTP listen address")
	fs.StringVar(&cliArgs.Mode, "mode", "http", "Proxy mode: http or stdio")
	fs.StringVar(&cliArgs.LogLevel, "log-level", "info", "Log level: debug, info, warn, error")
	fs.StringVar(&cliArgs.DBPath, "db", "", "SQLite database path (default: in-memory)")
	fs.StringVar(&cliArgs.ConfigPath, "config", "", "Server registry config JSON file")
	fs.StringVar(&cliArgs.Origins, "origins", "", "Comma-separated allowed origins")

	fs.Parse(args)

	return cliArgs
}

func printUsage() {
	flag.Usage()
}
