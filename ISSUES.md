# GitHub Issues to Create

## Issue 1: managed-mcp.json written to wrong location

**Title:** `managed-mcp.json written to wrong location`

### Problem

The `setup-mcp-gateway.sh` script writes `managed-mcp.json` to `~/.claude/managed-mcp.json`, but according to [Claude Code documentation](https://code.claude.com/docs/en/mcp.md), the correct system paths are:

- **Linux/WSL**: `/etc/claude-code/managed-mcp.json`
- **macOS**: `/Library/Application Support/ClaudeCode/managed-mcp.json`
- **Windows**: `C:\Program Files\ClaudeCode\managed-mcp.json`

These are system-wide paths (not user home directories) that require administrator privileges, designed to be deployed by IT administrators.

### Current behavior

```bash
MANAGED_MCP="${HOME}/.claude/managed-mcp.json"
```

### Expected behavior

Should detect the platform and write to the correct system location, or use a different approach for policy-based control.

### Impact

The `managed-mcp.json` file is likely being ignored by Claude Code since it's not in the expected location.

### References

- https://code.claude.com/docs/en/mcp.md#managed-mcp-configuration

---

## Issue 2: managed-mcp.json uses incorrect format

**Title:** `managed-mcp.json uses incorrect format`

### Problem

The `setup-mcp-gateway.sh` script writes `managed-mcp.json` with an incorrect format that doesn't match the Claude Code specification.

### Current format (incorrect)

```json
{
  "mcp": {
    "allowlist": ["armour"],
    "description": "Only Armour MCP server is allowed..."
  }
}
```

### Expected format per docs

For **exclusive control** with `managed-mcp.json`:
```json
{
  "mcpServers": {
    "armour": {
      "type": "stdio",
      "command": "/path/to/armour",
      "args": ["serve"]
    }
  }
}
```

For **policy-based control** (allowlists/denylists), this should be in the **managed settings file** (`managed-settings.json`), not `managed-mcp.json`:
```json
{
  "allowedMcpServers": [
    { "serverName": "armour" }
  ],
  "deniedMcpServers": []
}
```

### Impact

Claude Code will not recognize the current format, making the MCP policy ineffective.

### References

- https://code.claude.com/docs/en/mcp.md#option-1-exclusive-control-with-managed-mcpjson
- https://code.claude.com/docs/en/mcp.md#option-2-policy-based-control-with-allowlists-and-denylists

---

## Issue 3: Plugin .mcp.json files not discovered

**Title:** `Plugin .mcp.json files not discovered`

### Problem

According to Claude Code documentation, the **primary** method for plugins to define MCP servers is via a `.mcp.json` file at the plugin root. However, the discovery scripts don't scan for this file.

### Current behavior

The scripts scan for:
- `marketplace.json` with `plugins[].mcpServers`
- `plugin.json` with `mcpServers` field
- `server.json` with `remotes` array

### Missing

- `.mcp.json` at plugin root (primary method per docs)

### Expected behavior

Should also scan for `.mcp.json` files at plugin roots:

```
plugin-dir/
├── .claude-plugin/
│   └── plugin.json
├── .mcp.json          ← Should be discovered
└── ...
```

Format of `.mcp.json`:
```json
{
  "database-tools": {
    "command": "${CLAUDE_PLUGIN_ROOT}/servers/db-server",
    "args": ["--config", "${CLAUDE_PLUGIN_ROOT}/config.json"],
    "env": {
      "DB_URL": "${DB_URL}"
    }
  }
}
```

### References

- https://code.claude.com/docs/en/mcp.md#plugin-provided-mcp-servers
- https://code.claude.com/docs/en/plugins-reference.md#mcp-servers

---

## Issue 4: server.json format doesn't match official spec

**Title:** `server.json uses non-standard format`

### Problem

Armour uses a custom `server.json` format with a `remotes` array, but this doesn't appear in the official Claude Code documentation. The official plugin MCP server config uses `.mcp.json` or inline `mcpServers` in `plugin.json`.

### Current Armour format

```json
{
  "remotes": [
    { "type": "http", "url": "http://host:port" }
  ]
}
```

### Official formats

**`.mcp.json` at plugin root:**
```json
{
  "server-name": {
    "command": "${CLAUDE_PLUGIN_ROOT}/server",
    "args": [],
    "env": {}
  }
}
```

**Inline in `plugin.json`:**
```json
{
  "name": "my-plugin",
  "mcpServers": {
    "plugin-api": {
      "command": "${CLAUDE_PLUGIN_ROOT}/servers/api-server",
      "args": ["--port", "8080"]
    }
  }
}
```

### Recommendation

Consider whether `server.json` is a custom Armour extension or should be migrated to use the official `.mcp.json` format.

### References

- https://code.claude.com/docs/en/plugins-reference.md#mcp-servers
