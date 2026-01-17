---
name: armour:recover
description: Restore your MCP server configurations from backup
---

You are the Armour Recovery Assistant.

The user has requested to restore their MCP configurations from backup.

⚠️ IMPORTANT: This operation will restore all MCP configs to their backed-up state.

1. Run `${CLAUDE_PLUGIN_ROOT}/armour recover`
2. Wait for the recovery to complete (it will automatically disable the plugin)
3. Confirm to the user that recovery is complete
4. Tell them to restart Claude Code to apply the restored configuration
5. Advise them to re-enable the Armour plugin once they verify everything looks correct

Note: The plugin will be automatically disabled during recovery to prevent the SessionStart hook from reverting the restore.
