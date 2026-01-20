#!/bin/bash
# PreToolUse hook script - queries Armour rules server before native tool execution
# Returns JSON with decision: "allow" or "block"

ARMOUR_RULES_PORT="${ARMOUR_RULES_PORT:-8084}"
ARMOUR_RULES_URL="http://127.0.0.1:${ARMOUR_RULES_PORT}/api/check"

# Get tool name and input from environment (set by Claude Code hooks)
TOOL_NAME="${TOOL_NAME:-}"
TOOL_INPUT="${TOOL_INPUT:-}"

# URL-encode the content
urlencode() {
    python3 -c "import urllib.parse; print(urllib.parse.quote('''$1''', safe=''))"
}

# If no tool name, allow (shouldn't happen)
if [ -z "$TOOL_NAME" ]; then
    echo '{"decision": "allow"}'
    exit 0
fi

# Check if rules server is running
if ! curl -s --connect-timeout 0.1 "http://127.0.0.1:${ARMOUR_RULES_PORT}/api/health" > /dev/null 2>&1; then
    # Server not running - fail open (allow)
    echo '{"decision": "allow", "reason": "rules server not available"}'
    exit 0
fi

# Query the rules server
ENCODED_TOOL=$(urlencode "$TOOL_NAME")
ENCODED_CONTENT=$(urlencode "$TOOL_INPUT")

RESPONSE=$(curl -s --max-time 0.5 \
    "${ARMOUR_RULES_URL}?tool=${ENCODED_TOOL}&method=tools/call&content=${ENCODED_CONTENT}&scope=native" \
    2>/dev/null)

if [ -z "$RESPONSE" ]; then
    # No response - fail open
    echo '{"decision": "allow", "reason": "rules check timeout"}'
    exit 0
fi

# Return the response directly (it already has the right format)
echo "$RESPONSE"
