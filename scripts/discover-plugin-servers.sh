#!/bin/bash
# Discover MCP servers from ~/.claude.json and add them to ~/.armour/servers.json
# This enables Armour's stdio MCP server to proxy project-level MCP servers
# Note: Plugin MCP servers are NOT auto-discovered - configure them manually in ~/.armour/servers.json

set -e

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

log "Starting MCP server discovery..."

# Ensure config directory exists
mkdir -p "$ARMOUR_CONFIG_DIR"

# Initialize servers.json if it doesn't exist
if [ ! -f "$SERVERS_JSON" ]; then
  log "[Armour] Creating new servers.json..."
  cat > "$SERVERS_JSON" << 'JSON_EOF'
{
  "metadata": {
    "version": "1.0.16",
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

servers_json = os.path.expanduser("~/.armour/servers.json")
claude_config = os.path.expanduser("~/.claude.json")
project_hint = os.environ.get("CLAUDE_PROJECT_ROOT") or os.environ.get("PWD") or os.getcwd()

# Discovered servers (only from ~/.claude.json, NOT from plugins)
discovered = {}

def register_server(server_name, server_config, source_label, plugin_root=None):
  if not server_name or server_name == "armour" or server_name in discovered:
    return
  entry = {
    "name": server_name,
    "transport": server_config.get("type", "http"),
  }
  if "url" in server_config:
    entry["url"] = server_config["url"]
  if "command" in server_config:
    entry["transport"] = "stdio"
    entry["command"] = server_config["command"]
  if "args" in server_config:
    entry["args"] = server_config["args"]
  if "headers" in server_config and server_config["headers"]:
    entry["headers"] = server_config["headers"]
  if "env" in server_config and server_config["env"]:
    entry["env"] = server_config["env"]
  if plugin_root:
    env = entry.setdefault("env", {})
    env.setdefault("CLAUDE_PLUGIN_ROOT", str(plugin_root))
  discovered[server_name] = entry
  print(f"[Armour] Found: {server_name} ({source_label})", file=sys.stderr)

def resolve_mcp_servers(mcp_value, base_dir, source_label):
  if isinstance(mcp_value, dict):
    for server_name, server_config in mcp_value.items():
      if isinstance(server_config, dict):
        register_server(server_name, server_config, source_label, base_dir)
  elif isinstance(mcp_value, list):
    for server_config in mcp_value:
      if isinstance(server_config, dict) and "name" in server_config:
        register_server(server_config["name"], server_config, source_label, base_dir)
  elif isinstance(mcp_value, str):
    mcp_path = (base_dir / mcp_value).resolve()
    if mcp_path.is_file():
      try:
        with open(mcp_path) as f:
          config = json.load(f)
        resolve_mcp_servers(config.get("mcpServers", {}), base_dir, source_label)
      except Exception:
        pass

def add_project_mcp_servers():
  if not os.path.isfile(claude_config):
    return
  try:
    with open(claude_config) as f:
      data = json.load(f)
  except Exception:
    return

  projects = data.get("projects", {})
  project_entry = None
  if project_hint and project_hint in projects:
    project_entry = projects.get(project_hint)
  elif len(projects) == 1:
    # Safe fallback: use the sole project if only one exists
    project_entry = next(iter(projects.values()))

  if not isinstance(project_entry, dict):
    return

  mcp_servers = project_entry.get("mcpServers", {})
  if isinstance(mcp_servers, dict):
    for server_name, server_config in mcp_servers.items():
      if isinstance(server_config, dict):
        register_server(server_name, server_config, "claude.json project")

# Load servers.json first so we can check for existing servers
try:
  with open(servers_json) as f:
    config = json.load(f)
except Exception as e:
  print(f"[Armour] Error reading servers.json: {e}", file=sys.stderr)
  sys.exit(1)

# Get existing server names, entries, and indices
existing_servers = {}
existing_index = {}
for idx, server in enumerate(config.get("servers", [])):
  name = server.get("name")
  if name:
    existing_servers[name] = server
    existing_index[name] = idx

# Discover project-level MCP servers from ~/.claude.json
add_project_mcp_servers()

# Add discovered servers that aren't already there
added_count = 0
for server_name, server_config in discovered.items():
  if server_name in existing_index:
    config["servers"][existing_index[server_name]] = server_config
  else:
    config["servers"].append(server_config)
    added_count += 1

# Write back servers.json atomically (temp file + rename)
try:
  import tempfile
  dir_name = os.path.dirname(servers_json)
  fd, tmp_path = tempfile.mkstemp(dir=dir_name, suffix=".tmp")
  try:
    with os.fdopen(fd, "w") as f:
      json.dump(config, f, indent=2)
    os.rename(tmp_path, servers_json)
  except:
    os.unlink(tmp_path)
    raise
  if added_count > 0:
    print(f"[Armour] Added {added_count} discovered servers to {servers_json}", file=sys.stderr)
except Exception as e:
  print(f"[Armour] Error writing servers.json: {e}", file=sys.stderr)
  sys.exit(1)

# Disable discovered servers in Claude Code's config to prevent duplicate connections
# This must happen BEFORE Claude Code connects to MCP servers
def disable_servers_in_claude_config():
  if not os.path.isfile(claude_config):
    return

  try:
    with open(claude_config) as f:
      claude_data = json.load(f)
  except Exception:
    return

  projects = claude_data.get("projects", {})
  if not isinstance(projects, dict):
    return

  modified = False
  disabled_count = 0

  for project_path, project_config in projects.items():
    if not isinstance(project_config, dict):
      continue

    mcp_servers = project_config.get("mcpServers", {})
    if not isinstance(mcp_servers, dict):
      continue

    # Get or create disabledMcpServers list
    disabled = project_config.get("disabledMcpServers", [])
    if not isinstance(disabled, list):
      disabled = []

    disabled_set = set(disabled)

    # Disable servers that are being proxied by armour
    for server_name in discovered.keys():
      if server_name in mcp_servers and server_name not in disabled_set:
        disabled.append(server_name)
        disabled_set.add(server_name)
        modified = True
        disabled_count += 1
        print(f"[Armour] Disabled {server_name} in Claude config (proxied by armour)", file=sys.stderr)

    if disabled:
      project_config["disabledMcpServers"] = disabled

  if not modified:
    return

  # Write back Claude config atomically
  try:
    dir_name = os.path.dirname(claude_config)
    fd, tmp_path = tempfile.mkstemp(dir=dir_name, suffix=".tmp")
    try:
      with os.fdopen(fd, "w") as f:
        json.dump(claude_data, f, indent=2)
      os.rename(tmp_path, claude_config)
    except:
      os.unlink(tmp_path)
      raise
    print(f"[Armour] Disabled {disabled_count} servers in Claude config", file=sys.stderr)
  except Exception as e:
    print(f"[Armour] Error updating Claude config: {e}", file=sys.stderr)

disable_servers_in_claude_config()
PYTHON_EOF

log "[Armour] ✓ MCP servers discovered and configured"
