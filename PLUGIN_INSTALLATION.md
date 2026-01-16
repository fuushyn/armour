# Sentinel MCP Proxy Plugin - Installation Guide

## Overview

The Sentinel MCP Proxy plugin automatically registers itself with Claude Code via the SessionStart hook. Users don't need to do any additional configuration - it just works!

## What Happens on Plugin Install

When a user installs the plugin via `/plugin install mcp-go-proxy`:

1. **Plugin loads** - Claude Code loads the plugin from `.claude-plugin/`
2. **Manifest is read** - `plugin.json` is parsed, including hooks configuration
3. **Hooks are registered** - Claude Code registers the SessionStart hook
4. **First session starts** - The hook automatically runs:
   - Creates `~/.claude/mcp-proxy/` directory
   - Initializes `servers.json` with default settings
   - Adds sentinel entry to `~/.claude/.mcp.json`
   - Appears in `/mcp` tools panel immediately

## SessionStart Hook Details

**File**: `.claude-plugin/hooks/sync-mcp-servers.sh`  
**Trigger**: SessionStart (every time Claude Code starts)  
**Actions**:
- Initializes sentinel proxy (first run only)
- Syncs servers added via `claude mcp add` to registry
- Removes synced servers from project configs

## User Experience

### First Install
```bash
/plugin install mcp-go-proxy
# Plugin installs...
# Restart Claude or start new session...
# Sentinel proxy now appears in /mcp!
```

### Adding New Servers
```bash
claude mcp add --transport http context7 https://api.example.com
# Restart Claude (or next session)...
# Server automatically appears through sentinel proxy!
```

## Installation Checklist

The plugin is production-ready when:

- ✅ `plugin.json` includes `"hooks": { "path": "hooks/hooks.json" }`
- ✅ `hooks/hooks.json` defines SessionStart hook
- ✅ `hooks/sync-mcp-servers.sh` is executable
- ✅ Hook script creates sentinel entry on first run
- ✅ Hook script syncs servers on every run
- ✅ Documentation explains the hook behavior
- ✅ README includes troubleshooting section

## Verification Commands

Users can verify the hook is working:

```bash
# Check hook installation
cat ~/.claude-plugin/hooks/hooks.json

# Check hook permissions
ls -la ~/.claude-plugin/hooks/sync-mcp-servers.sh

# Manually run the hook
bash ~/.claude-plugin/hooks/sync-mcp-servers.sh

# Verify sentinel is configured
cat ~/.claude/.mcp.json | grep -A 5 sentinel

# Check server registry
cat ~/.claude/mcp-proxy/servers.json | jq '.servers[] | {name, transport}'
```

## Deployment Notes

The plugin is ready for:
- ✅ Installation via `/plugin install`
- ✅ Distribution via Claude Marketplace (if applicable)
- ✅ Manual installation from repo

No additional setup or configuration needed - the SessionStart hook handles everything automatically!

## For Developers

If making changes to the hook:

1. Test locally: `bash .claude-plugin/hooks/sync-mcp-servers.sh`
2. Ensure it's executable: `chmod +x .claude-plugin/hooks/sync-mcp-servers.sh`
3. Verify plugin.json references it: `cat .claude-plugin/plugin.json | grep hooks`
4. Document any changes in `.claude-plugin/HOOKS.md`

