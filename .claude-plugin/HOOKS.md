# SessionStart Hook

This plugin includes an automatic **SessionStart hook** that runs every time you start a Claude Code session.

## What It Does

The hook automatically sets up Sentinel on first install and synchronizes servers on every session:

1. **Initializes Sentinel** (first run only):
   - Creates `~/.claude/mcp-proxy/` directory
   - Initializes `servers.json` registry with default settings
   - Adds sentinel entry to `~/.claude/.mcp.json` so it appears in MCP tools
2. **Syncs servers** (every SessionStart):
   - Detects servers added via `claude mcp add`
   - Syncs them to `~/.claude/mcp-proxy/servers.json`
   - Removes duplicates from project configs (so they only appear through the proxy)

## Installation

The hook is **automatically installed** when you install the plugin:

- **Hook file**: `.claude-plugin/hooks/hooks.json`
- **Hook script**: `.claude-plugin/hooks/sync-mcp-servers.sh`
- **Configuration**: Referenced in `.claude-plugin/plugin.json`

Claude Code automatically detects and registers hooks when the plugin is installed.

## Verification

To verify the hook is working:

### 1. Check Hook Installation
```bash
# Verify hook configuration exists
cat ~/.claude-plugin/hooks/hooks.json

# Verify hook script is executable
ls -la ~/.claude-plugin/hooks/sync-mcp-servers.sh
```

### 2. Manual Sync Test
```bash
# Run the hook manually to test
bash ~/.claude-plugin/hooks/sync-mcp-servers.sh

# Check the registry was created/updated
cat ~/.claude/mcp-proxy/servers.json | jq '.servers[] | {name, transport}'
```

### 3. Live Test
```bash
# Add a new server via standard Claude Code workflow
claude mcp add --transport http my-server https://api.example.com

# Restart Claude Code (or wait for next SessionStart)

# Verify the server was synced to the registry
cat ~/.claude/mcp-proxy/servers.json | grep my-server

# Verify it was removed from project config
cat ~/.claude.json | grep -A 5 '"mcpServers"' | grep -v my-server
```

## How It Works

When Claude Code starts:

1. Claude Code loads the plugin from `.claude-plugin/plugin.json`
2. It reads the hooks configuration: `hooks.path: "hooks/hooks.json"`
3. It runs the SessionStart hook: `sync-mcp-servers.sh`
4. The script scans `~/.claude.json` for servers added via `claude mcp add`
5. Any new servers are synced to `~/.claude/mcp-proxy/servers.json`
6. Synced servers are removed from project `mcpServers` to avoid duplication

## Troubleshooting

### Hook not running?

1. **Check hook registration**: `cat ~/.claude-plugin/hooks/hooks.json`
2. **Check script permissions**: `ls -la ~/.claude-plugin/hooks/sync-mcp-servers.sh`
   - Should show `x` permission (executable)
3. **Run manually**: `bash ~/.claude-plugin/hooks/sync-mcp-servers.sh`
4. **Check for errors**: Review Claude Code debug logs

### Servers not being synced?

1. **Verify registry exists**: `ls -la ~/.claude/mcp-proxy/servers.json`
2. **Check claude.json**: `cat ~/.claude.json | jq '.projects | keys'`
3. **Run sync script**: `bash ~/.claude-plugin/hooks/sync-mcp-servers.sh`
4. **Check registry output**: `cat ~/.claude/mcp-proxy/servers.json`

### Need to re-sync manually?

```bash
# The hook runs automatically on SessionStart
# To force a sync, run it manually:
bash ~/.claude-plugin/hooks/sync-mcp-servers.sh

# Then restart Claude Code
```

## Hook Lifecycle

- **Installed**: Automatically when you install the plugin
- **Triggered**: Every time Claude Code starts (SessionStart hook)
- **Executed**: The `sync-mcp-servers.sh` script runs silently
- **Silent**: On success, the hook produces no output
- **Idempotent**: Safe to run multiple times (no side effects)

## Implementation Details

The hook is implemented as:

- **Hook Type**: `command`
- **Hook Trigger**: `SessionStart`
- **Command**: `${CLAUDE_PLUGIN_ROOT}/hooks/sync-mcp-servers.sh`
- **Language**: Bash + Python
- **Behavior**: Idempotent (safe to run repeatedly)

The script:
1. Creates `~/.claude/mcp-proxy/` directory if it doesn't exist
2. Initializes registry with default settings on first run
3. Reads `~/.claude.json` for all project servers
4. Compares against existing registry entries
5. Adds new servers to registry
6. Removes synced servers from project config
7. Writes updated registry and config files

No configuration needed - it just works!
