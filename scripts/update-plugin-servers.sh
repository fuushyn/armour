#!/bin/bash
# Update Armour's plugin.json with discovered plugin MCP servers
# This adds all discovered servers to the mcpServers array so Claude Code
# loads them when Armour plugin is enabled

set -e

PLUGINS_DIR="${HOME}/.claude/plugins"
PLUGIN_JSON="${CLAUDE_PLUGIN_ROOT:-.}/.claude-plugin/plugin.json"

log() {
  if [ "${QUIET:-0}" = "0" ]; then
    echo -e "$@"
  fi
}

log "[Armour] Discovering plugin MCP servers..."

# Use Python to safely parse and update plugin.json
python3 << 'PYTHON_EOF'
import json
import os
import sys
from pathlib import Path

plugins_dir = os.path.expanduser("~/.claude/plugins")
plugin_json_path = os.environ.get("PLUGIN_JSON", "./.claude-plugin/plugin.json")

# Discovered servers to add
discovered = {}

# Scan plugins directory
if os.path.isdir(plugins_dir):
    for root, dirs, files in os.walk(plugins_dir):
        if os.path.basename(root) == ".claude-plugin":
            # Check marketplace.json
            if "marketplace.json" in files:
                try:
                    with open(os.path.join(root, "marketplace.json")) as f:
                        manifest = json.load(f)
                        if "plugins" in manifest:
                            for plugin in manifest["plugins"]:
                                if "mcpServers" in plugin:
                                    for server_name, server_config in plugin["mcpServers"].items():
                                        if server_name != "armour" and server_name not in discovered:
                                            discovered[server_name] = {
                                                "name": server_name,
                                                "type": server_config.get("type", "http"),
                                            }
                                            if "url" in server_config:
                                                discovered[server_name]["url"] = server_config["url"]
                                            if "headers" in server_config and server_config["headers"]:
                                                discovered[server_name]["headers"] = server_config["headers"]
                                            print(f"[Armour] Found: {server_name} (marketplace)", file=sys.stderr)
                except Exception as e:
                    pass

            # Check plugin.json
            if "plugin.json" in files:
                try:
                    with open(os.path.join(root, "plugin.json")) as f:
                        manifest = json.load(f)
                        if "mcpServers" in manifest and isinstance(manifest["mcpServers"], list):
                            for server_config in manifest["mcpServers"]:
                                server_name = server_config.get("name")
                                if server_name and server_name != "armour" and server_name not in discovered:
                                    discovered[server_name] = {
                                        "name": server_name,
                                        "type": server_config.get("type", "http"),
                                    }
                                    if "url" in server_config:
                                        discovered[server_name]["url"] = server_config["url"]
                                    if "command" in server_config:
                                        discovered[server_name]["command"] = server_config["command"]
                                    if "args" in server_config:
                                        discovered[server_name]["args"] = server_config["args"]
                                    print(f"[Armour] Found: {server_name} (plugin.json)", file=sys.stderr)
                except Exception as e:
                    pass

# Load Armour's plugin.json
try:
    with open(plugin_json_path) as f:
        armour_config = json.load(f)
except FileNotFoundError:
    print(f"[Armour] Error: {plugin_json_path} not found", file=sys.stderr)
    sys.exit(1)
except Exception as e:
    print(f"[Armour] Error reading plugin.json: {e}", file=sys.stderr)
    sys.exit(1)

# Initialize mcpServers if not present
if "mcpServers" not in armour_config:
    armour_config["mcpServers"] = []

# Find or create Armour's own server declaration
armour_server = None
for server in armour_config["mcpServers"]:
    if server.get("name") == "armour":
        armour_server = server
        break

if not armour_server:
    armour_server = {
        "name": "armour",
        "type": "stdio",
        "command": "${CLAUDE_PLUGIN_ROOT}/mcp-proxy",
        "args": ["-mode", "stdio"]
    }
    armour_config["mcpServers"].insert(0, armour_server)

# Get existing server names
existing_names = {s.get("name") for s in armour_config["mcpServers"]}

# Add discovered servers that aren't already there
added_count = 0
for server_name, server_config in discovered.items():
    if server_name not in existing_names:
        armour_config["mcpServers"].append(server_config)
        existing_names.add(server_name)
        added_count += 1

# Write back plugin.json
try:
    with open(plugin_json_path, "w") as f:
        json.dump(armour_config, f, indent=2)
    if added_count > 0:
        print(f"[Armour] Added {added_count} discovered servers to plugin.json", file=sys.stderr)
except Exception as e:
    print(f"[Armour] Error writing plugin.json: {e}", file=sys.stderr)
    sys.exit(1)
PYTHON_EOF

log "[Armour] ✓ Plugin servers updated"
