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
    "version": "1.0.14",
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
from urllib.parse import urlparse

plugins_dir = os.path.expanduser("~/.claude/plugins")
servers_json = os.path.expanduser("~/.armour/servers.json")
claude_config = os.path.expanduser("~/.claude.json")
project_hint = os.environ.get("CLAUDE_PROJECT_ROOT") or os.environ.get("PWD") or os.getcwd()

# Discovered servers
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

def normalize_remote_transport(remote_type, url):
  if isinstance(remote_type, str) and remote_type:
    remote_type = remote_type.lower()
    if remote_type in ("sse", "http", "stdio"):
      return remote_type
  if isinstance(url, str) and url.startswith(("http://", "https://")):
    return "http"
  return None

def same_host(left, right):
  try:
    return urlparse(left).netloc.lower() == urlparse(right).netloc.lower()
  except Exception:
    return False

def apply_remote_override(remote, plugin_name, existing_servers, existing_index, config):
  remote_url = remote.get("url") if isinstance(remote, dict) else None
  if not remote_url:
    return

  for name, entry in list(discovered.items()):
    if entry.get("url") and same_host(entry["url"], remote_url):
      transport = normalize_remote_transport(remote.get("type"), remote_url)
      if transport:
        entry["transport"] = transport
      entry["url"] = remote_url
      return

  # Check existing servers for same-host match before adding new entry
  for name, entry in existing_servers.items():
    if entry.get("url") and same_host(entry["url"], remote_url):
      transport = normalize_remote_transport(remote.get("type"), remote_url)
      if transport:
        entry["transport"] = transport
      entry["url"] = remote_url
      config["servers"][existing_index[name]] = entry
      return

  # Fall back: add a new entry if nothing matched
  name = plugin_name or "remote"
  if name in discovered or name in existing_servers:
    return
  transport = normalize_remote_transport(remote.get("type"), remote_url) or "http"
  discovered[name] = {"name": name, "transport": transport, "url": remote_url}

def apply_server_json(plugin_root, existing_servers, existing_index, config):
  server_json = plugin_root / "server.json"
  if not server_json.is_file():
    return
  try:
    data = json.loads(server_json.read_text())
  except Exception:
    return
  remotes = data.get("remotes") or []
  if not isinstance(remotes, list):
    return
  for remote in remotes:
    if isinstance(remote, dict):
      apply_remote_override(remote, plugin_root.name, existing_servers, existing_index, config)

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

# Scan plugins directory
if os.path.isdir(plugins_dir):
  for root, dirs, files in os.walk(plugins_dir):
    if os.path.basename(root) == ".claude-plugin":
      plugin_root = Path(root).parent
      plugin_dir_name = plugin_root.name
      # Check marketplace.json
      if "marketplace.json" in files:
        try:
          with open(os.path.join(root, "marketplace.json")) as f:
            manifest = json.load(f)
            if "plugins" in manifest:
              for plugin in manifest["plugins"]:
                plugin_name = plugin.get("name")
                if plugin_name and plugin_name != plugin_dir_name:
                  continue
                base_dir = plugin_root
                source = plugin.get("source")
                if isinstance(source, str) and source and not source.startswith("/") and "://" not in source:
                  base_dir = (plugin_root / source).resolve()
                mcp_value = plugin.get("mcpServers")
                if mcp_value is not None:
                  resolve_mcp_servers(mcp_value, base_dir, "marketplace")
                apply_server_json(base_dir, existing_servers, existing_index, config)
        except Exception:
          pass

      # Check plugin.json
      if "plugin.json" in files:
        try:
          with open(os.path.join(root, "plugin.json")) as f:
            manifest = json.load(f)
            mcp_servers = manifest.get("mcpServers")
            if mcp_servers is not None:
              resolve_mcp_servers(mcp_servers, plugin_root, "plugin.json")
            apply_server_json(plugin_root, existing_servers, existing_index, config)
        except Exception as e:
          pass

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
PYTHON_EOF

log "[Armour] ✓ Plugin servers discovered and configured"
