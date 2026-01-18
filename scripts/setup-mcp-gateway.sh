#!/bin/bash
# SessionStart hook: Auto-discover plugin MCP servers and set up Armour as the gateway
# This script:
# 1. Scans installed Claude Code plugins for MCP server configurations
# 2. Extracts MCP server configs from plugin.json and marketplace.json
# 3. Merges them into Armour's servers.json
# 4. Creates managed-mcp.json allowlist to disable direct plugin MCP connections
#
# Can be called from:
# - Plugin installation script (verbose output)
# - SessionStart hook (silent mode when ARMOUR_QUIET=1)

set -e

# Check if running in quiet mode (from SessionStart hook)
QUIET="${ARMOUR_QUIET:-0}"
log() {
  if [ "$QUIET" = "0" ]; then
    echo -e "$@"
  fi
}

PLUGINS_DIR="${HOME}/.claude/plugins"
ARMOUR_CONFIG_DIR="${HOME}/.armour"
SERVERS_JSON="${ARMOUR_CONFIG_DIR}/servers.json"
MANAGED_MCP="${HOME}/.claude/managed-mcp.json"

# Helper: Extract MCP servers from marketplace.json
extract_marketplace_mcp_servers() {
  local manifest_file=$1

  if [ ! -f "$manifest_file" ]; then
    return 0
  fi

  # Use Python for reliable JSON parsing
  python3 << 'PYTHON_EOF'
import json
import sys

try:
  manifest_file = sys.argv[1]
  with open(manifest_file) as f:
    manifest = json.load(f)

  # marketplace.json has plugins array
  if "plugins" in manifest and isinstance(manifest["plugins"], list):
    for plugin in manifest["plugins"]:
      if "mcpServers" in plugin and isinstance(plugin["mcpServers"], dict):
        plugin_name = plugin.get("name", "unknown")
        for server_name, server_config in plugin["mcpServers"].items():
          # Output as JSON for parsing by bash
          output = {
            "plugin_name": plugin_name,
            "server_name": server_name,
            "server_config": server_config
          }
          print(json.dumps(output))
except Exception as e:
  pass
PYTHON_EOF
}

# Helper: Extract MCP servers from plugin.json
extract_plugin_mcp_servers() {
  local manifest_file=$1

  if [ ! -f "$manifest_file" ]; then
    return 0
  fi

  python3 << 'PYTHON_EOF'
import json
import sys

try:
  manifest_file = sys.argv[1]
  with open(manifest_file) as f:
    manifest = json.load(f)

  # plugin.json has mcpServers array
  if "mcpServers" in manifest and isinstance(manifest["mcpServers"], list):
    plugin_name = manifest.get("name", "unknown")
    for server_config in manifest["mcpServers"]:
      if isinstance(server_config, dict) and "name" in server_config:
        server_name = server_config["name"]
        output = {
          "plugin_name": plugin_name,
          "server_name": server_name,
          "server_config": server_config
        }
        print(json.dumps(output))
except Exception as e:
  pass
PYTHON_EOF
}

# Ensure Armour config directory exists
mkdir -p "$ARMOUR_CONFIG_DIR"

# Initialize servers.json if it doesn't exist
if [ ! -f "$SERVERS_JSON" ]; then
  cat > "$SERVERS_JSON" << 'JSON_EOF'
{
  "metadata": {
    "version": "1.0.0",
    "auto_generated": true,
    "description": "Armour MCP Proxy - Auto-discovered servers"
  },
  "policy": {
    "mode": "moderate"
  },
  "servers": []
}
JSON_EOF
fi

# Scan plugins directory for MCP server configurations
discovered_servers=()

if [ -d "$PLUGINS_DIR" ]; then
  log "[Armour] Scanning plugins for MCP servers..."

  # Find all plugin.json and marketplace.json files
  while IFS= read -r manifest_file; do
    if [ ! -f "$manifest_file" ]; then
      continue
    fi

    # Determine manifest type
    filename=$(basename "$manifest_file")
    parent_dir=$(basename $(dirname "$manifest_file"))

    # Only process files in .claude-plugin directories
    if [ "$parent_dir" != ".claude-plugin" ]; then
      continue
    fi

    log "[Armour] Processing: $manifest_file"

    if [ "$filename" = "marketplace.json" ]; then
      extract_marketplace_mcp_servers "$manifest_file"
    elif [ "$filename" = "plugin.json" ]; then
      extract_plugin_mcp_servers "$manifest_file"
    fi
  done < <(find "$PLUGINS_DIR" -type f \( -name "plugin.json" -o -name "marketplace.json" \) 2>/dev/null)
fi

# Merge discovered servers into servers.json
python3 << 'PYTHON_EOF'
import json
import sys
import os

servers_json = os.path.expanduser("~/.armour/servers.json")
plugins_dir = os.path.expanduser("~/.claude/plugins")

try:
  # Load existing servers.json
  with open(servers_json) as f:
    config = json.load(f)

  existing_servers = {s["name"]: s for s in config.get("servers", [])}

  # Track discovered servers
  discovered = {}

  # Scan for MCP servers in plugins
  if os.path.isdir(plugins_dir):
    for root, dirs, files in os.walk(plugins_dir):
      # Look for plugin.json and marketplace.json in .claude-plugin directories
      if os.path.basename(root) == ".claude-plugin":

        if "marketplace.json" in files:
          with open(os.path.join(root, "marketplace.json")) as f:
            try:
              manifest = json.load(f)
              if "plugins" in manifest:
                for plugin in manifest["plugins"]:
                  if "mcpServers" in plugin:
                    for server_name, server_config in plugin["mcpServers"].items():
                      if server_name not in existing_servers and server_name != "armour":
                        discovered[server_name] = {
                          "name": server_name,
                          "transport": server_config.get("type", "http"),
                          "url": server_config.get("url"),
                          "headers": server_config.get("headers", {}),
                        }
                        if "url" not in server_config and "command" in server_config:
                          discovered[server_name]["transport"] = "stdio"
                          discovered[server_name]["command"] = server_config["command"]
                          if "args" in server_config:
                            discovered[server_name]["args"] = server_config["args"]
                        print(f"[Armour] Discovered: {server_name} from marketplace", file=sys.stderr)
            except:
              pass

        if "plugin.json" in files:
          with open(os.path.join(root, "plugin.json")) as f:
            try:
              manifest = json.load(f)
              if "mcpServers" in manifest:
                for server_config in manifest["mcpServers"]:
                  server_name = server_config.get("name")
                  if server_name and server_name not in existing_servers and server_name != "armour":
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
                    print(f"[Armour] Discovered: {server_name} from plugin", file=sys.stderr)
            except:
              pass

  # Add discovered servers to existing ones
  if discovered:
    for server_name, server_config in discovered.items():
      if server_name not in existing_servers:
        config["servers"].append(server_config)

    # Write back servers.json
    with open(servers_json, "w") as f:
      json.dump(config, f, indent=2)

    print(f"[Armour] Added {len(discovered)} discovered servers to {servers_json}", file=sys.stderr)

except Exception as e:
  print(f"[Armour] Error processing servers: {e}", file=sys.stderr)
  sys.exit(1)
PYTHON_EOF

# Create/update managed-mcp.json to allowlist only armour
log "[Armour] Setting up managed-mcp.json..."
cat > "$MANAGED_MCP" << 'JSON_EOF'
{
  "mcp": {
    "allowlist": ["armour"],
    "description": "Only Armour MCP server is allowed. All discovered plugin MCP servers are proxied through Armour for unified security policies."
  }
}
JSON_EOF

log "[Armour] ✓ Completed MCP gateway setup"
log "[Armour] - Servers configured in: $SERVERS_JSON"
log "[Armour] - MCP policy configured in: $MANAGED_MCP"
