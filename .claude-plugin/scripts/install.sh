#!/bin/bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║           MCP Go Proxy - Installation Script                   ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}\n"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}✗ Go is not installed${NC}"
    echo "Please install Go 1.21+ from https://golang.org/dl/"
    exit 1
fi

echo -e "${GREEN}✓${NC} Go is installed"

# Get the plugin root directory
PLUGIN_ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"
PROJECT_ROOT="$( cd "$PLUGIN_ROOT/.." && pwd )"

echo -e "${BLUE}Plugin Root: ${PLUGIN_ROOT}${NC}"
echo -e "${BLUE}Project Root: ${PROJECT_ROOT}${NC}\n"

# Build the proxy binary
echo -e "${YELLOW}Building proxy binary...${NC}"
cd "$PROJECT_ROOT"

if go build -o "$PLUGIN_ROOT/mcp-proxy" .; then
    echo -e "${GREEN}✓${NC} Binary built successfully"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi

# Make binary executable
chmod +x "$PLUGIN_ROOT/mcp-proxy"
echo -e "${GREEN}✓${NC} Binary is executable"

# Create config directory
CONFIG_DIR="$HOME/.claude/mcp-proxy"
mkdir -p "$CONFIG_DIR"
echo -e "${GREEN}✓${NC} Config directory created: $CONFIG_DIR"

# Initialize empty config if it doesn't exist
if [ ! -f "$CONFIG_DIR/servers.json" ]; then
    echo '{"servers":[]}' > "$CONFIG_DIR/servers.json"
    echo -e "${GREEN}✓${NC} Empty servers.json created"
else
    echo -e "${YELLOW}⚠${NC} servers.json already exists, skipping"
fi

# Create database directory
mkdir -p "$CONFIG_DIR"
echo -e "${GREEN}✓${NC} Database directory ready"

# Print next steps
echo -e "\n${GREEN}Installation Complete!${NC}\n"
echo "Next steps:"
echo "1. In Claude Code, run: /sentinel setup"
echo "2. Your MCP servers will be auto-detected and protected"
echo "3. Restart Claude Code to apply the security layer\n"

echo "Optional commands:"
echo "  mcp-proxy detect          - Show detected MCP servers"
echo "  mcp-proxy up              - Auto-discover project servers"
echo "  mcp-proxy -mode http      - Run as HTTP server on :8080"
echo "  mcp-proxy -mode stdio     - Run as stdio MCP server\n"

echo -e "${BLUE}For help: mcp-proxy help${NC}"
echo -e "${BLUE}Docs: https://github.com/yourusername/mcp-go-proxy${NC}\n"
