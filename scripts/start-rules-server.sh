#!/bin/bash
# Start the Armour rules server if not already running

set -e

ARMOUR_RULES_PORT="${ARMOUR_RULES_PORT:-8084}"
ARMOUR_DIR="${HOME}/.armour"
PID_FILE="${ARMOUR_DIR}/rules-server.pid"
LOG_FILE="${ARMOUR_DIR}/rules-server.log"
DB_FILE="${ARMOUR_DIR}/rules.db"

# Get the armour binary path
ARMOUR_BIN="${CLAUDE_PLUGIN_ROOT}/armour"
if [ ! -x "$ARMOUR_BIN" ]; then
    # Fallback to PATH
    ARMOUR_BIN=$(which armour 2>/dev/null || echo "")
fi

if [ -z "$ARMOUR_BIN" ] || [ ! -x "$ARMOUR_BIN" ]; then
    echo "[Armour] Rules server binary not found" >&2
    exit 0  # Don't fail - just skip
fi

# Ensure armour directory exists
mkdir -p "$ARMOUR_DIR"

# Check if server is already running
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE" 2>/dev/null)
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        # Check if it's actually our server
        if curl -s --connect-timeout 0.5 "http://127.0.0.1:${ARMOUR_RULES_PORT}/api/health" > /dev/null 2>&1; then
            echo "[Armour] Rules server already running (PID $OLD_PID)"
            exit 0
        fi
    fi
    # Stale PID file, remove it
    rm -f "$PID_FILE"
fi

# Start the rules server in daemon mode
echo "[Armour] Starting rules server on port $ARMOUR_RULES_PORT..."
nohup "$ARMOUR_BIN" serve \
    -port "$ARMOUR_RULES_PORT" \
    -db "$DB_FILE" \
    >> "$LOG_FILE" 2>&1 &

NEW_PID=$!
echo "$NEW_PID" > "$PID_FILE"

# Wait briefly and verify it started
sleep 0.5
if curl -s --connect-timeout 1 "http://127.0.0.1:${ARMOUR_RULES_PORT}/api/health" > /dev/null 2>&1; then
    echo "[Armour] Rules server started (PID $NEW_PID)"
else
    echo "[Armour] Rules server may have failed to start, check $LOG_FILE" >&2
fi
