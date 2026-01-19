#!/bin/bash
# Debug script for armour plugin - logs plugin loading and MCP server startup

LOG_DIR="$HOME/.armour"
LOG_FILE="$LOG_DIR/plugin-debug.log"

mkdir -p "$LOG_DIR"

{
  echo "=== Armour Plugin Debug Log ==="
  echo "Timestamp: $(date)"
  echo "Claude Version: $(claude --version 2>&1 || echo 'unknown')"
  echo ""

  echo "=== Plugin Root Check ==="
  echo "CLAUDE_PLUGIN_ROOT: ${CLAUDE_PLUGIN_ROOT}"
  if [ -n "$CLAUDE_PLUGIN_ROOT" ]; then
    echo "Plugin root exists: $([ -d "$CLAUDE_PLUGIN_ROOT" ] && echo 'YES' || echo 'NO')"
    echo "Contents of plugin root:"
    ls -la "$CLAUDE_PLUGIN_ROOT" | head -20
  fi
  echo ""

  echo "=== plugin.json Check ==="
  if [ -f "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json" ]; then
    echo "✓ plugin.json found"
    echo "Contents:"
    cat "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json"
  else
    echo "✗ plugin.json NOT found at $CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json"
  fi
  echo ""

  echo "=== Commands Check ==="
  if [ -d "$CLAUDE_PLUGIN_ROOT/commands" ]; then
    echo "✓ commands/ directory found"
    ls -la "$CLAUDE_PLUGIN_ROOT/commands"
  else
    echo "✗ commands/ directory NOT found"
  fi
  echo ""

  echo "=== Hooks Check ==="
  if [ -f "$CLAUDE_PLUGIN_ROOT/hooks/hooks.json" ]; then
    echo "✓ hooks.json found"
    cat "$CLAUDE_PLUGIN_ROOT/hooks/hooks.json"
  else
    echo "✗ hooks/hooks.json NOT found"
  fi
  echo ""

  echo "=== MCP Server Check ==="
  if [ -f "$CLAUDE_PLUGIN_ROOT/armour" ]; then
    echo "✓ armour binary found at $CLAUDE_PLUGIN_ROOT/armour"
    echo "Binary is executable: $( [ -x "$CLAUDE_PLUGIN_ROOT/armour" ] && echo 'YES' || echo 'NO')"
    echo "Binary size: $(ls -lh "$CLAUDE_PLUGIN_ROOT/armour" | awk '{print $5}')"
  else
    echo "✗ armour binary NOT found"
  fi
  echo ""

  echo "=== Servers Config ==="
  if [ -f "$HOME/.armour/servers.json" ]; then
    echo "✓ ~/.armour/servers.json found"
    echo "First 20 lines:"
    head -20 "$HOME/.armour/servers.json"
  else
    echo "✗ ~/.armour/servers.json NOT found"
  fi
  echo ""

  echo "=== Claude Settings ==="
  if [ -f "$HOME/.claude/settings.json" ]; then
    echo "✓ ~/.claude/settings.json found"
    grep -A5 "enabledPlugins\|armour" "$HOME/.claude/settings.json" || echo "No armour plugin settings found"
  else
    echo "✗ ~/.claude/settings.json NOT found"
  fi

} | tee -a "$LOG_FILE"

echo ""
echo "Debug log saved to: $LOG_FILE"
echo "View with: tail -f $LOG_FILE"
