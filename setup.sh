#!/bin/bash

# Sentinel MCP Proxy - Transparent Setup Script
# This script builds the proxy and migrates your MCP servers to a secure registry.

set -e

# Colors for better visibility
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}🛡️  Sentinel MCP Proxy Setup${NC}"
echo -e "=============================="

# 1. Build the binary
echo -e "\n${YELLOW}1. Building Sentinel binary...${NC}"
if go build -o sentinel main.go; then
    echo -e "${GREEN}✓ Binary built successfully: ./sentinel${NC}"
else
    echo -e "❌ Build failed. Please ensure Go is installed."
    exit 1
fi

# 2. Explain the process
echo -e "\n${YELLOW}2. What this script does:${NC}"
echo -e "   - Detects existing MCP servers in your Claude configuration."
echo -e "   - Migrates them to a secure registry at ~/.claude/mcp-proxy/servers.json."
echo -e "   - Updates ~/.claude/.mcp.json to use Sentinel as a security proxy."
echo -e "   - Backs up your original configuration."

echo -e "\n${YELLOW}This adds a security layer that blocks destructive commands (like 'rm -rf') and provides a dashboard.${NC}"

# 3. Get User Consent
read -p "Do you want to proceed with the migration? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "\n${YELLOW}Setup cancelled. You can still run the proxy manually:${NC}"
    echo -e "  ./sentinel -mode stdio -config <your-config.json>"
    exit 0
fi

# 4. Execute Migration
echo -e "\n${YELLOW}3. Executing migration...${NC}"
./sentinel migrate

# 5. Final Instructions
echo -e "\n${GREEN}✨ Setup Complete!${NC}"
echo -e "1. ${BLUE}Restart Claude Code${NC} to apply the security layer."
echo -e "2. Access your security dashboard at: ${BLUE}http://localhost:13337${NC}"
echo -e "3. All your tools are now protected by Sentinel."

echo -e "\n${YELLOW}To uninstall:${NC} cp ~/.claude/.mcp.json.backup.* ~/.claude/.mcp.json
