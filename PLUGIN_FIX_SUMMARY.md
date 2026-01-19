# Armour Plugin Structure Fix

## Changes Made

### 1. Plugin Structure Corrected
Following Claude Code plugin specification, the plugin structure has been reorganized:

**Before (broken):**
```
armour/
└── .claude-plugin/
    ├── plugin.json
    ├── commands/        ❌ Should NOT be here
    ├── hooks/           ❌ Should NOT be here
    ├── scripts/         ❌ Should NOT be here
    ├── armour           ❌ Should NOT be here
    └── ...
```

**After (fixed):**
```
armour/                     ← plugin root
├── .claude-plugin/
│   ├── plugin.json         ← plugin manifest
│   └── marketplace.json    ← marketplace catalog
├── commands/               ✓ at root level
├── hooks/                  ✓ at root level
├── scripts/                ✓ at root level
├── armour                  ✓ binary at root level
└── mcp-proxy               ✓ binary at root level
```

### 2. Cleanup
Removed extra files from `.claude-plugin/`:
- ✓ Removed `.mcp.json` (configuration is in plugin.json)
- ✓ Removed `HOOKS.md` and `README.md` (documentation)
- ✓ Kept only `plugin.json` and `marketplace.json` in `.claude-plugin/`

### 3. Manifest Fixes
- ✓ `mcpServers` now uses the object map schema (Claude Code spec)
- ✓ Removed `hooks` entry to avoid duplicate hook loading

### 4. Logging Added
Added comprehensive logging for debugging:

**New Files:**
- `scripts/debug-plugin.sh` - Complete plugin diagnostics script

**Enhanced Files:**
- `scripts/enable-plugin.sh` - Added logging to `~/.armour/hooks.log`
- `scripts/discover-plugin-servers.sh` - Added file logging to `~/.armour/hooks.log`

**Log Locations:**
- Session hook logs: `~/.armour/hooks.log`
- Full debug output: `~/.armour/plugin-debug.log`

## Testing

### 1. Run the Debug Script
Before starting Claude Code, run the debug script to verify setup:
```bash
/Users/devel12/dev/armour/scripts/debug-plugin.sh
```

This will output a comprehensive report and save it to `~/.armour/plugin-debug.log`.

### 2. Start Claude Code with Plugin
```bash
claude --plugin-dir /Users/devel12/dev/armour/
```

### 3. Check Plugin Status
Inside Claude Code, run:
```
/plugin
```

Navigate to **Installed** tab → look for `armour` under **Local** scope (should show as enabled).

### 4. Check MCP Server
Inside Claude Code, run:
```
/mcp
```

Look for `plugin:armour:armour` in the MCP servers list (should show as ✓ connected).

### 5. Verify Commands
Run:
```
/armour:backup
```

Should see help for the backup command.

### 6. Review Logs
Check what happened during startup:
```bash
tail -f ~/.armour/hooks.log
```

## Verification Checklist

- [ ] `.claude-plugin/` contains `plugin.json` and `marketplace.json` only
- [ ] `commands/`, `hooks/`, `scripts/` are at plugin root level
- [ ] `armour` and `mcp-proxy` binaries are at plugin root level
- [ ] `claude --plugin-dir /Users/devel12/dev/armour/` loads the plugin
- [ ] Plugin appears in `/plugin` → Installed tab
- [ ] MCP server connects in `/mcp`
- [ ] Logs appear in `~/.armour/hooks.log`

## If Issues Persist

1. Run the debug script:
   ```bash
   /Users/devel12/dev/armour/scripts/debug-plugin.sh
   ```

2. Check logs:
   ```bash
   cat ~/.armour/hooks.log
   cat ~/.armour/plugin-debug.log
   ```

3. Clear plugin cache and restart:
   ```bash
   rm -rf ~/.claude/plugins/cache
   claude --plugin-dir /Users/devel12/dev/armour/
   ```

## Files Changed

- ✓ Moved: `commands/`, `hooks/`, `scripts/`, `armour`, `mcp-proxy` to root level
- ✓ Cleaned: `.claude-plugin/` directory (kept marketplace.json)
- ✓ Created: `scripts/debug-plugin.sh`
- ✓ Updated: `scripts/enable-plugin.sh` - added logging
- ✓ Updated: `scripts/discover-plugin-servers.sh` - added file logging
- ✓ Created: `PLUGIN_FIX_SUMMARY.md` (this file)

## Ready to Test

The structure is now correct according to Claude Code specification. Test with:
```bash
claude --plugin-dir /Users/devel12/dev/armour/
```
