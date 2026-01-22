#!/usr/bin/env bash
set -euo pipefail

# Simple animated discovery runner for Armour
# Uses the built armour binary if present, otherwise falls back to `go run .`.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARMOUR_BIN="${ARMOUR_BIN:-$REPO_ROOT/armour}"
TMP_OUT="$(mktemp)"
SPINNER=('-' '\\' '|' '/')

cleanup() { rm -f "$TMP_OUT"; }
trap cleanup EXIT

if [[ -x "$ARMOUR_BIN" ]]; then
  RUN_CMD=("$ARMOUR_BIN" detect --json)
else
  RUN_CMD=(go run . detect --json)
fi

echo "🔎 Discovering MCP servers and placing them behind Armour..."

set +e
"${RUN_CMD[@]}" >"$TMP_OUT" 2>/dev/null &
PID=$!

i=0
while kill -0 "$PID" 2>/dev/null; do
  printf "\r%s scanning for stdio/http/SSE servers..." "${SPINNER[$i]}"
  i=$(( (i + 1) % ${#SPINNER[@]} ))
  sleep 0.12
done
wait "$PID"
STATUS=$?
printf "\r"
set -e

if [[ $STATUS -ne 0 ]]; then
  echo "❌ Discovery failed (exit $STATUS)"
  exit $STATUS
fi

echo "✅ Discovery complete. Summary:"
cat "$TMP_OUT" | sed 's/^/  /'
echo "You can edit ~/.armour/servers.json or launch the dashboard for more details."
