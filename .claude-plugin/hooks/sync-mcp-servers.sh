#!/bin/bash
# Auto-sync MCP servers added via 'claude mcp add' to Armour registry
# This runs on SessionStart and:
# 1. Migrates old sentinel registry if it exists
# 2. Scans all projects for new servers added via 'claude mcp add'
# 3. Syncs them to the armour registry
# 4. Removes them from project config so all connections route through armour
#
# This hook is automatically installed with the armour plugin.
# See ~/.claude-plugin/README.md for troubleshooting.

set -e

HOME_DIR="$HOME"
CLAUDE_CONFIG="$HOME_DIR/.claude.json"
ARMOUR_REGISTRY="$HOME_DIR/.armour/servers.json"
ARMOUR_DIR="$HOME_DIR/.armour"
OLD_SENTINEL_REGISTRY="$HOME_DIR/.claude/mcp-proxy/servers.json"

# Create armour directory if it doesn't exist
if [ ! -d "$ARMOUR_DIR" ]; then
  mkdir -p "$ARMOUR_DIR"
fi

# Migrate from old sentinel registry if it exists and new one doesn't
if [ -f "$OLD_SENTINEL_REGISTRY" ] && [ ! -f "$ARMOUR_REGISTRY" ]; then
  cp "$OLD_SENTINEL_REGISTRY" "$ARMOUR_REGISTRY"
fi

# Initialize registry if it doesn't exist
if [ ! -f "$ARMOUR_REGISTRY" ]; then
  python3 << 'INIT_PYTHON'
import json
import os
registry = {
  "metadata": {
    "version": "1.0.0"
  },
  "policy": {
    "mode": "moderate"
  },
  "servers": []
}
registry_path = os.path.expanduser("~/.armour/servers.json")
os.makedirs(os.path.dirname(registry_path), exist_ok=True)
with open(registry_path, "w") as f:
  json.dump(registry, f, indent=2)
INIT_PYTHON
fi

# Only proceed if claude config exists
if [ ! -f "$CLAUDE_CONFIG" ]; then
  exit 0
fi

# Python script to setup armour proxy and sync servers
python3 << 'EOF'
import json
import os
import shutil

home = os.path.expanduser("~")
claude_config_path = os.path.join(home, ".claude.json")
mcp_config_path = os.path.join(home, ".claude", ".mcp.json")
registry_path = os.path.join(home, ".armour", "servers.json")

# CLAUDE_PLUGIN_ROOT is provided by Claude Code's hook execution context
# It points to the plugin root directory
plugin_root = os.environ.get("CLAUDE_PLUGIN_ROOT", os.path.expanduser("~/.claude/plugins/cache/armour-marketplace/armour/1.0.0"))
proxy_binary = os.path.join(plugin_root, "armour")

# Initialize or update armour proxy in ~/.claude/.mcp.json
def setup_armour_proxy():
    try:
        with open(mcp_config_path) as f:
            mcp_config = json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        mcp_config = {"mcpServers": {}}

    # Check if armour is already configured
    if "armour" not in mcp_config.get("mcpServers", {}):
        mcp_config["mcpServers"] = mcp_config.get("mcpServers", {})
        mcp_config["mcpServers"]["armour"] = {
            "command": proxy_binary,
            "args": ["-mode", "stdio", "-config", registry_path],
            "env": {
                "ARMOUR_CONFIG": registry_path,
                "ARMOUR_POLICY": "moderate"
            },
            "description": "Armour Security Proxy"
        }

        # Ensure directory exists
        os.makedirs(os.path.dirname(mcp_config_path), exist_ok=True)

        with open(mcp_config_path, "w") as f:
            json.dump(mcp_config, f, indent=2)

setup_armour_proxy()

# Now sync servers
home = os.path.expanduser("~")
claude_config_path = os.path.join(home, ".claude.json")
registry_path = os.path.join(home, ".armour", "servers.json")

# Read Claude config with all projects
try:
    with open(claude_config_path) as f:
        claude_config = json.load(f)
    all_projects = claude_config.get("projects", {})
except (FileNotFoundError, json.JSONDecodeError):
    all_projects = {}

# Read sentinel registry
try:
    with open(registry_path) as f:
        registry = json.load(f)
    existing_servers = {s["name"]: s for s in registry.get("servers", [])}
except (FileNotFoundError, json.JSONDecodeError):
    registry = {"servers": [], "policy": {"mode": "moderate"}, "metadata": {"version": "1.0.0"}}
    existing_servers = {}

# Scan all projects for servers to sync
synced_any = False
servers_to_remove = []  # Track which servers to remove from project config

for project_path, project_config in all_projects.items():
    project_servers = project_config.get("mcpServers", {})

    for server_name, server_config in project_servers.items():
        # Skip armour to avoid recursion, and old proxy names
        if server_name in ["armour", "sentinel", "mcp-proxy", "mcp-go-proxy"]:
            continue

        # Skip if already in registry
        if server_name in existing_servers:
            # Still mark for removal from project config
            servers_to_remove.append((project_path, server_name))
            continue

        # Convert Claude format to Sentinel format
        sentinel_entry = {"name": server_name}

        # Map type to transport
        if "type" in server_config:
            sentinel_entry["transport"] = server_config["type"]
        elif "url" in server_config:
            sentinel_entry["transport"] = "http"
            sentinel_entry["url"] = server_config["url"]
        else:
            sentinel_entry["transport"] = "stdio"

        # Copy other fields
        if "command" in server_config:
            sentinel_entry["command"] = server_config["command"]
        if "url" in server_config:
            sentinel_entry["url"] = server_config["url"]
        if "args" in server_config:
            sentinel_entry["args"] = server_config["args"]
        if "env" in server_config:
            sentinel_entry["env"] = server_config["env"]

        registry["servers"].append(sentinel_entry)
        existing_servers[server_name] = sentinel_entry
        synced_any = True
        servers_to_remove.append((project_path, server_name))

# Remove synced servers from project config
config_changed = False
for project_path, server_name in servers_to_remove:
    if project_path in all_projects:
        mcp_servers = all_projects[project_path].get("mcpServers", {})
        if server_name in mcp_servers:
            del mcp_servers[server_name]
            config_changed = True

# Write sentinel registry if anything was synced
if synced_any:
    with open(registry_path, "w") as f:
        json.dump(registry, f, indent=2)

# Write claude config if anything was removed
if config_changed:
    with open(claude_config_path, "w") as f:
        json.dump(claude_config, f, indent=2)
EOF

