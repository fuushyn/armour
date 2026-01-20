#!/bin/bash
# PreToolUse hook script - queries Armour rules server before native tool execution
# Returns JSON with hookEventName and permissionDecision

ARMOUR_RULES_PORT="${ARMOUR_RULES_PORT:-8084}"
ARMOUR_RULES_URL="http://127.0.0.1:${ARMOUR_RULES_PORT}/api/check"

# Get tool name and input from environment (set by Claude Code hooks)
TOOL_NAME="${TOOL_NAME:-}"
TOOL_INPUT="${TOOL_INPUT:-}"

# URL-encode the content
urlencode() {
    python3 -c "import urllib.parse; print(urllib.parse.quote('''$1''', safe=''))"
}

# Helper to output PreToolUse hook response
output_response() {
    local decision="$1"
    local reason="$2"
    if [ -n "$reason" ]; then
        echo "{\"hookEventName\": \"PreToolUse\", \"permissionDecision\": \"$decision\", \"permissionDecisionReason\": \"$reason\"}"
    else
        echo "{\"hookEventName\": \"PreToolUse\", \"permissionDecision\": \"$decision\"}"
    fi
}

# If no tool name, allow (shouldn't happen)
if [ -z "$TOOL_NAME" ]; then
    output_response "allow"
    exit 0
fi

# Check if rules server is running
if ! curl -s --connect-timeout 0.1 "http://127.0.0.1:${ARMOUR_RULES_PORT}/api/health" > /dev/null 2>&1; then
    # Server not running - fail open (allow)
    output_response "allow" "rules server not available"
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
    output_response "allow" "rules check timeout"
    exit 0
fi

# Parse the response from rules server and convert to hook format
# The rules server returns {"allowed": true/false, "reason": "..."}
ALLOWED=$(echo "$RESPONSE" | python3 -c "import sys, json; data=json.load(sys.stdin); print('true' if data.get('allowed', True) else 'false')" 2>/dev/null)
REASON=$(echo "$RESPONSE" | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('reason', ''))" 2>/dev/null)

if [ "$ALLOWED" = "true" ]; then
    output_response "allow"
else
    # For blocked content, use "deny" to block or "ask" to prompt user
    output_response "deny" "$REASON"
fi
