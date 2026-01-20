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
from pathlib import Path
from urllib.parse import urlparse

servers_json = os.path.expanduser("~/.armour/servers.json")
plugins_dir = os.path.expanduser("~/.claude/plugins")

try:
  # Load existing servers.json
  with open(servers_json) as f:
    config = json.load(f)

  existing_servers = {s["name"]: s for s in config.get("servers", [])}
  existing_index = {s["name"]: idx for idx, s in enumerate(config.get("servers", [])) if s.get("name")}

  # Track discovered servers
  discovered = {}

  def register_server(server_name, server_config, source_label, plugin_root=None):
    if not server_name or server_name in existing_servers or server_name in discovered or server_name == "armour":
      return

    entry = {
      "name": server_name,
      "transport": server_config.get("type", "http"),
    }

    if "command" in server_config:
      entry["transport"] = "stdio"
      entry["command"] = server_config["command"]
      if "args" in server_config:
        entry["args"] = server_config["args"]

    if "url" in server_config:
      entry["url"] = server_config["url"]

    if "headers" in server_config and server_config["headers"]:
      entry["headers"] = server_config["headers"]

    if "env" in server_config and server_config["env"]:
      entry["env"] = server_config["env"]

    if plugin_root:
      env = entry.setdefault("env", {})
      env.setdefault("CLAUDE_PLUGIN_ROOT", str(plugin_root))

    discovered[server_name] = entry
    print(f"[Armour] Discovered: {server_name} from {source_label}", file=sys.stderr)

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

  def apply_remote_override(remote, plugin_name):
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
    for name, entry in list(existing_servers.items()):
      if entry.get("url") and same_host(entry["url"], remote_url):
        transport = normalize_remote_transport(remote.get("type"), remote_url)
        if transport:
          entry["transport"] = transport
        entry["url"] = remote_url
        config["servers"][existing_index[name]] = entry
        return

    name = plugin_name or "remote"
    if name in discovered or name in existing_servers:
      return
    transport = normalize_remote_transport(remote.get("type"), remote_url) or "http"
    discovered[name] = {"name": name, "transport": transport, "url": remote_url}

  def apply_server_json(plugin_root):
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
        apply_remote_override(remote, plugin_root.name)

  def resolve_mcp_servers(mcp_value, base_dir, source_label):
    if isinstance(mcp_value, dict):
      for name, cfg in mcp_value.items():
        if isinstance(cfg, dict):
          register_server(name, cfg, source_label, base_dir)
    elif isinstance(mcp_value, list):
      for cfg in mcp_value:
        if isinstance(cfg, dict) and "name" in cfg:
          register_server(cfg["name"], cfg, source_label, base_dir)
    elif isinstance(mcp_value, str):
      mcp_path = (base_dir / mcp_value).resolve()
      if mcp_path.is_file():
        try:
          with open(mcp_path) as f:
            mcp_config = json.load(f)
          resolve_mcp_servers(mcp_config.get("mcpServers", {}), base_dir, source_label)
        except:
          pass

  # Scan for MCP servers in plugins
  if os.path.isdir(plugins_dir):
    for root, dirs, files in os.walk(plugins_dir):
      if os.path.basename(root) == ".claude-plugin":
        plugin_root = Path(root).parent
        plugin_dir_name = plugin_root.name

        if "marketplace.json" in files:
          with open(os.path.join(root, "marketplace.json")) as f:
            try:
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
                  apply_server_json(base_dir)
            except:
              pass

        if "plugin.json" in files:
          with open(os.path.join(root, "plugin.json")) as f:
            try:
              manifest = json.load(f)
              mcp_value = manifest.get("mcpServers")
              if mcp_value is not None:
                resolve_mcp_servers(mcp_value, plugin_root, "plugin")
              apply_server_json(plugin_root)
            except:
              pass

  # Add discovered servers to existing ones
  if discovered:
    for server_name, server_config in discovered.items():
      if server_name in existing_index:
        config["servers"][existing_index[server_name]] = server_config
      else:
        config["servers"].append(server_config)

    # Write back servers.json atomically (temp file + rename)
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
