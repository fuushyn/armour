#!/bin/bash
# Discover plugin MCP servers and add them to ~/.armour/servers.json
# This enables Armour's stdio MCP server to proxy all discovered plugin servers

set -e

PLUGINS_DIR="${HOME}/.claude/plugins"
ARMOUR_CONFIG_DIR="${HOME}/.armour"
SERVERS_JSON="${ARMOUR_CONFIG_DIR}/servers.json"
LOG_FILE="${ARMOUR_CONFIG_DIR}/hooks.log"

log() {
  local msg="[$(date +'%Y-%m-%d %H:%M:%S')] [discover-servers] $*"
  if [ "${QUIET:-0}" = "0" ]; then
    echo -e "$msg"
  fi
  echo "$msg" >> "$LOG_FILE" 2>/dev/null || true
}

log "Starting plugin server discovery..."

# Ensure config directory exists
mkdir -p "$ARMOUR_CONFIG_DIR"

# Initialize servers.json if it doesn't exist
if [ ! -f "$SERVERS_JSON" ]; then
  log "[Armour] Creating new servers.json..."
  cat > "$SERVERS_JSON" << 'JSON_EOF'
{
  "metadata": {
    "version": "1.0.0",
    "description": "Armour MCP Proxy - Backend servers configuration"
  },
  "policy": {
    "mode": "moderate"
  },
  "servers": []
}
JSON_EOF
fi

# Use Python to discover and update servers
python3 << 'PYTHON_EOF'
import json
import os
import sys
from pathlib import Path

plugins_dir = os.path.expanduser("~/.claude/plugins")
servers_json = os.path.expanduser("~/.armour/servers.json")

# Discovered servers
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
                        "transport": server_config.get("type", "http"),
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
          plugin_root = Path(root).parent
          with open(os.path.join(root, "plugin.json")) as f:
            manifest = json.load(f)
            mcp_servers = manifest.get("mcpServers")
            if isinstance(mcp_servers, dict):
              for server_name, server_config in mcp_servers.items():
                if server_name and server_name != "armour" and server_name not in discovered:
                  discovered[server_name] = {
                    "name": server_name,
                    "transport": server_config.get("type", "http"),
                  }
                  if "url" in server_config:
                    discovered[server_name]["url"] = server_config["url"]
                  if "command" in server_config:
                    discovered[server_name]["command"] = server_config["command"]
                  if "args" in server_config:
                    discovered[server_name]["args"] = server_config["args"]
                  print(f"[Armour] Found: {server_name} (plugin.json)", file=sys.stderr)
            elif isinstance(mcp_servers, list):
              for server_config in mcp_servers:
                server_name = server_config.get("name")
                if server_name and server_name != "armour" and server_name not in discovered:
                  discovered[server_name] = {
                    "name": server_name,
                    "transport": server_config.get("type", "http"),
                  }
                  if "url" in server_config:
                    discovered[server_name]["url"] = server_config["url"]
                  if "command" in server_config:
                    discovered[server_name]["command"] = server_config["command"]
                  if "args" in server_config:
                    discovered[server_name]["args"] = server_config["args"]
                  print(f"[Armour] Found: {server_name} (plugin.json)", file=sys.stderr)
            elif isinstance(mcp_servers, str):
              mcp_path = (plugin_root / mcp_servers).resolve()
              if mcp_path.is_file():
                with open(mcp_path) as f:
                  config = json.load(f)
                for server_name, server_config in config.get("mcpServers", {}).items():
                  if server_name and server_name != "armour" and server_name not in discovered:
                    discovered[server_name] = {
                      "name": server_name,
                      "transport": server_config.get("type", "http"),
                    }
                    if "url" in server_config:
                      discovered[server_name]["url"] = server_config["url"]
                    if "command" in server_config:
                      discovered[server_name]["command"] = server_config["command"]
                    if "args" in server_config:
                      discovered[server_name]["args"] = server_config["args"]
                    print(f"[Armour] Found: {server_name} (plugin.json mcpServers path)", file=sys.stderr)
        except Exception as e:
          pass

# Load servers.json
try:
  with open(servers_json) as f:
    config = json.load(f)
except Exception as e:
  print(f"[Armour] Error reading servers.json: {e}", file=sys.stderr)
  sys.exit(1)

# Get existing server names
existing_names = {s.get("name") for s in config.get("servers", [])}

# Add discovered servers that aren't already there
added_count = 0
for server_name, server_config in discovered.items():
  if server_name not in existing_names:
    config["servers"].append(server_config)
    existing_names.add(server_name)
    added_count += 1

# Write back servers.json
try:
  with open(servers_json, "w") as f:
    json.dump(config, f, indent=2)
  if added_count > 0:
    print(f"[Armour] Added {added_count} discovered servers to {servers_json}", file=sys.stderr)
except Exception as e:
  print(f"[Armour] Error writing servers.json: {e}", file=sys.stderr)
  sys.exit(1)
PYTHON_EOF

log "[Armour] ✓ Plugin servers discovered and configured"
