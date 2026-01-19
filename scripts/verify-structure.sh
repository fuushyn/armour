#!/bin/bash
# Verify plugin structure is correct according to Claude Code spec

cd /Users/devel12/dev/armour

echo "=== Armour Plugin Structure Verification ==="
echo ""

PASS=0
FAIL=0

check() {
  local condition=$1
  local msg=$2
  if [ "$condition" = "true" ]; then
    echo "✓ $msg"
    ((PASS++))
  else
    echo "✗ $msg"
    ((FAIL++))
  fi
}

# Check .claude-plugin/ structure
echo "1. Checking .claude-plugin/ directory:"
check "$([ -f '.claude-plugin/plugin.json' ] && echo true || echo false)" "plugin.json exists"
check "$([ -f '.claude-plugin/marketplace.json' ] && echo true || echo false)" "marketplace.json exists"
check "$([ ! -f '.claude-plugin/.mcp.json' ] && echo true || echo false)" ".mcp.json removed"
check "$([ ! -d '.claude-plugin/commands' ] && echo true || echo false)" "commands/ not in .claude-plugin/"
check "$([ ! -d '.claude-plugin/hooks' ] && echo true || echo false)" "hooks/ not in .claude-plugin/"
echo ""

# Check root level structure
echo "2. Checking plugin root level:"
check "$([ -d 'commands' ] && echo true || echo false)" "commands/ directory exists at root"
check "$([ -d 'hooks' ] && echo true || echo false)" "hooks/ directory exists at root"
check "$([ -d 'scripts' ] && echo true || echo false)" "scripts/ directory exists at root"
check "$([ -f 'armour' ] && echo true || echo false)" "armour binary exists at root"
check "$([ -f 'mcp-proxy' ] && echo true || echo false)" "mcp-proxy binary exists at root"
echo ""

# Check command files
echo "3. Checking command files:"
check "$([ -f 'commands/backup.md' ] && echo true || echo false)" "commands/backup.md exists"
check "$([ -f 'commands/recover.md' ] && echo true || echo false)" "commands/recover.md exists"
check "$([ -f 'commands/status.md' ] && echo true || echo false)" "commands/status.md exists"
echo ""

# Check hook files
echo "4. Checking hook files:"
check "$([ -f 'hooks/hooks.json' ] && echo true || echo false)" "hooks/hooks.json exists"
echo ""

# Check binary executability
echo "5. Checking binary permissions:"
check "$([ -x 'armour' ] && echo true || echo false)" "armour is executable"
check "$([ -x 'mcp-proxy' ] && echo true || echo false)" "mcp-proxy is executable"
echo ""

# Validate plugin.json
echo "6. Validating plugin.json:"
if python3 -c "import json; json.load(open('.claude-plugin/plugin.json'))" 2>/dev/null; then
  echo "✓ plugin.json is valid JSON"
  ((PASS++))

  # Check referenced paths
  echo ""
  echo "   Checking referenced paths in plugin.json:"

  check "$([ -f './commands/backup.md' ] && echo true || echo false)" "   - ./commands/backup.md (in plugin.json)"
  check "$([ -f './commands/recover.md' ] && echo true || echo false)" "   - ./commands/recover.md (in plugin.json)"
  check "$([ -f './commands/status.md' ] && echo true || echo false)" "   - ./commands/status.md (in plugin.json)"
else
  echo "✗ plugin.json is invalid JSON"
  ((FAIL++))
fi
echo ""

# Summary
echo "=== Summary ==="
echo "Passed: $PASS"
echo "Failed: $FAIL"
echo ""

if [ $FAIL -eq 0 ]; then
  echo "✓ Structure is CORRECT! Ready to test with:"
  echo "  claude --plugin-dir /Users/devel12/dev/armour/"
  exit 0
else
  echo "✗ Structure has issues. See failures above."
  exit 1
fi
