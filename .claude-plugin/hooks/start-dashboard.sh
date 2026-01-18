#!/bin/bash
# Start Armour dashboard HTTP server on SessionStart hook

# Kill any existing processes
pkill -f "armour.*-mode http" 2>/dev/null || true

# Start the dashboard server in HTTP mode
nohup "${CLAUDE_PLUGIN_ROOT}/armour" \
  -mode http \
  -listen 127.0.0.1:13337 \
  -config "${HOME}/.armour/servers.json" \
  -log-level info \
  > "${HOME}/.armour/dashboard.log" 2>&1 &

# Give it a moment to start
sleep 1

# Verify it's running
if curl -s http://127.0.0.1:13337/ > /dev/null 2>&1; then
  echo "✅ Armour dashboard started on http://127.0.0.1:13337"
else
  echo "⚠️ Armour dashboard may not have started properly"
fi
