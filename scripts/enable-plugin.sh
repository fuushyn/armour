#!/bin/bash
# Enable the armour plugin on session start if it's not already enabled

set +e

LOG_DIR="$HOME/.armour"
LOG_FILE="$LOG_DIR/hooks.log"
mkdir -p "$LOG_DIR"

log() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] [enable-plugin] $*" >> "$LOG_FILE"
}

log "Starting plugin enable hook"
log "CLAUDE_PLUGIN_ROOT: $CLAUDE_PLUGIN_ROOT"

# For now, just log that we tried - don't modify settings
# since SessionStart hooks run after plugin loading
log "Hook executed (plugin state managed by Claude Code)"

exit 0
