#!/bin/bash
#
# SafeHooks Installer
# Security rules for AI coding assistants - blocks dangerous commands, protects sensitive files
#
# Usage: curl -fsSL https://safehooks.dev/install | bash
#
# Auto-detects and configures:
#   - Claude Code / Claude Desktop / Claude Cowork
#   - Cursor
#   - Windsurf (Codeium Cascade)
#   - Gemini CLI
#   - GitHub Copilot
#   - Amp Code
#   - OpenCode
#   - Codex CLI
#

set -e

VERSION="1.0.0"
SAFEHOOKS_DIR="$HOME/.safehooks"
REPO_RAW_URL="${SAFEHOOKS_URL:-https://raw.githubusercontent.com/anthropics/armour/main/safehooks}"

# Colors (TTY only)
if [ -t 1 ]; then
    RED='\033[0;31m' GREEN='\033[0;32m' YELLOW='\033[1;33m'
    BLUE='\033[0;34m' DIM='\033[2m' NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' DIM='' NC=''
fi

# Parse arguments
SPECIFIC_TOOL=""
ACTION="install"

while [[ $# -gt 0 ]]; do
    case $1 in
        --tool)     SPECIFIC_TOOL="$2"; shift 2 ;;
        --uninstall) ACTION="uninstall"; shift ;;
        --update)   ACTION="update"; shift ;;
        --list)     ACTION="list"; shift ;;
        --help|-h)
            cat << 'EOF'
SafeHooks Installer - Security rules for AI coding assistants

Usage: curl -fsSL https://safehooks.dev/install | bash
       ./install.sh [options]

Options:
  --tool <name>    Install for specific tool only
  --uninstall      Remove SafeHooks from all tools
  --update         Update patterns only
  --list           List detected tools
  --help           Show this help

Supported tools:
  claude-code, cursor, windsurf, gemini-cli,
  github-copilot, amp-code, opencode, codex-cli
EOF
            exit 0 ;;
        *) echo -e "${RED}Unknown: $1${NC}"; exit 1 ;;
    esac
done

log_info()    { echo -e "${BLUE}→${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warn()    { echo -e "${YELLOW}!${NC} $1"; }
log_error()   { echo -e "${RED}✗${NC} $1"; }
log_dim()     { echo -e "${DIM}  $1${NC}"; }

check_jq() {
    command -v jq &>/dev/null || {
        log_error "jq required: brew install jq / apt install jq"
        exit 1
    }
}

download() {
    curl -fsSL "$1" -o "$2" 2>/dev/null || wget -q "$1" -O "$2" 2>/dev/null || {
        log_error "Download failed: $1"
        return 1
    }
}

install_patterns() {
    log_info "Installing patterns to ~/.safehooks/"
    mkdir -p "$SAFEHOOKS_DIR"
    download "${REPO_RAW_URL}/block-patterns.json" "$SAFEHOOKS_DIR/block-patterns.json"
    download "${REPO_RAW_URL}/allow-patterns.json" "$SAFEHOOKS_DIR/allow-patterns.json"
    download "${REPO_RAW_URL}/tool-rules.json" "$SAFEHOOKS_DIR/tool-rules.json"
    log_success "Patterns installed"
}

install_validator() {
    cat > "$SAFEHOOKS_DIR/validator.py" << 'SCRIPT'
#!/usr/bin/env python3
"""SafeHooks Validator - Security validation for AI coding assistants."""
import json, re, sys, os

SAFEHOOKS_DIR = os.path.expanduser("~/.safehooks")

def load_json(f):
    try:
        with open(os.path.join(SAFEHOOKS_DIR, f)) as fp:
            return json.load(fp)
    except: return []

def check(cmd, patterns, block=False):
    for p in patterns:
        try:
            if re.search(p["pattern"], cmd, re.IGNORECASE):
                return (True, p) if block else True
        except: pass
    return (False, None) if block else False

def validate_cmd(data):
    # Handle different input formats from various tools
    cmd = (data.get("tool_input", {}).get("command") or
           data.get("arguments", {}).get("command") or
           data.get("tool_info", {}).get("command_line") or
           data.get("tool_info", {}).get("command") or
           data.get("toolArgs", {}).get("command") if isinstance(data.get("toolArgs"), dict) else None or
           "")

    # Try parsing toolArgs if it's a JSON string (GitHub Copilot format)
    if not cmd and isinstance(data.get("toolArgs"), str):
        try:
            args = json.loads(data["toolArgs"])
            cmd = args.get("command", "")
        except: pass

    if not cmd: return 0, None

    if check(cmd, load_json("allow-patterns.json")):
        return 0, None

    blocked, p = check(cmd, load_json("block-patterns.json"), block=True)
    if blocked:
        return 2, f"BLOCKED [{p.get('category','security')}]: {p.get('reason','Blocked by SafeHooks')}"
    return 0, None

def validate_file(data):
    path = (data.get("tool_input", {}).get("file_path") or
            data.get("tool_info", {}).get("file_path") or
            data.get("arguments", {}).get("file_path") or "")
    if not path: return 0, None

    sensitive = [
        (r"\.env($|\.)", "Environment file"),
        (r"\.ssh/", "SSH directory"),
        (r"\.aws/credentials", "AWS credentials"),
        (r"(^|/)\.git/config$", "Git config"),
        (r"(^|/)secrets?\.json$", "Secrets file"),
    ]
    for pat, name in sensitive:
        if re.search(pat, path, re.IGNORECASE):
            return 2, f"BLOCKED: {name} access blocked"
    return 0, None

def main():
    try:
        data = json.load(sys.stdin)
    except: sys.exit(1)

    # Determine tool type from various formats
    tool = (data.get("tool_name") or data.get("tool") or
            data.get("toolName") or data.get("agent_action_name") or "").lower()

    # Map various tool names to validation type
    if tool in ("bash", "shell", "exec", "run_shell_command", "pre_run_command", "beforeshellexecution"):
        code, msg = validate_cmd(data)
    elif tool in ("read", "write", "edit", "write_file", "read_file", "pre_read_code", "pre_write_code", "beforereadfile"):
        code, msg = validate_file(data)
    else:
        code, msg = 0, None

    if msg: print(msg, file=sys.stderr)
    sys.exit(code)

if __name__ == "__main__": main()
SCRIPT
    chmod +x "$SAFEHOOKS_DIR/validator.py"
    log_success "Validator installed"
}

INSTALLED=0

#=============================================================================
# CLAUDE CODE / CLAUDE DESKTOP / CLAUDE COWORK
#=============================================================================
detect_claude() { [ -d "$HOME/.claude" ]; }

install_claude() {
    local f="$HOME/.claude/settings.json"
    mkdir -p "$HOME/.claude"

    if [ -f "$f" ]; then
        jq -e '.hooks.PreToolUse[]? | select(.hooks[]?.command | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "Claude Code: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            .hooks = (.hooks // {}) |
            .hooks.PreToolUse = (.hooks.PreToolUse // []) + [
                {"matcher": "Bash", "hooks": [{"type": "command", "command": $cmd}]},
                {"matcher": "Read|Write|Edit", "hooks": [{"type": "command", "command": $cmd}]}
            ]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"hooks":{"PreToolUse":[
  {"matcher":"Bash","hooks":[{"type":"command","command":"$SAFEHOOKS_DIR/validator.py"}]},
  {"matcher":"Read|Write|Edit","hooks":[{"type":"command","command":"$SAFEHOOKS_DIR/validator.py"}]}
]}}
EOF
    fi
    log_success "Claude Code/Desktop/Cowork configured"
    log_dim "Settings: $f"
    ((INSTALLED++))
}

uninstall_claude() {
    local f="$HOME/.claude/settings.json"
    [ ! -f "$f" ] && return
    jq 'if .hooks.PreToolUse then .hooks.PreToolUse = [.hooks.PreToolUse[] |
        select(.hooks[]?.command | contains("safehooks") | not)] else . end |
        if .hooks.PreToolUse == [] then del(.hooks.PreToolUse) else . end |
        if .hooks == {} then del(.hooks) else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "Claude Code: removed"
}

#=============================================================================
# CURSOR (supports both preToolUse and beforeShellExecution)
#=============================================================================
detect_cursor() { [ -d "$HOME/.cursor" ] || [ -d "$HOME/Library/Application Support/Cursor" ]; }

install_cursor() {
    local f="$HOME/.cursor/hooks.json"
    mkdir -p "$HOME/.cursor"

    if [ -f "$f" ]; then
        jq -e '.hooks | to_entries[] | select(.value[]?.command | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "Cursor: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            .hooks = (.hooks // {}) |
            .hooks.preToolUse = (.hooks.preToolUse // []) + [{"command": $cmd, "timeout": 30}] |
            .hooks.beforeShellExecution = (.hooks.beforeShellExecution // []) + [{"command": $cmd, "timeout": 30}] |
            .hooks.beforeReadFile = (.hooks.beforeReadFile // []) + [{"command": $cmd, "timeout": 30}]
        ' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"version":1,"hooks":{
  "preToolUse":[{"command":"$SAFEHOOKS_DIR/validator.py","timeout":30}],
  "beforeShellExecution":[{"command":"$SAFEHOOKS_DIR/validator.py","timeout":30}],
  "beforeReadFile":[{"command":"$SAFEHOOKS_DIR/validator.py","timeout":30}]
}}
EOF
    fi
    log_success "Cursor configured (preToolUse + beforeShellExecution)"
    log_dim "Settings: $f"
    ((INSTALLED++))
}

uninstall_cursor() {
    local f="$HOME/.cursor/hooks.json"
    [ ! -f "$f" ] && return
    jq 'if .hooks then .hooks |= with_entries(
        .value = [.value[]? | select(.command | contains("safehooks") | not)]
    ) | .hooks |= with_entries(select(.value | length > 0)) else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "Cursor: removed"
}

#=============================================================================
# WINDSURF (Codeium Cascade)
#=============================================================================
detect_windsurf() { [ -d "$HOME/.codeium" ] || [ -d "/Applications/Windsurf.app" ]; }

install_windsurf() {
    local f="$HOME/.codeium/windsurf/hooks.json"
    mkdir -p "$HOME/.codeium/windsurf"

    if [ -f "$f" ]; then
        jq -e '.hooks | to_entries[] | select(.value[]?.command | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "Windsurf: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            .hooks = (.hooks // {}) |
            .hooks.pre_run_command = (.hooks.pre_run_command // []) + [{"command": $cmd}] |
            .hooks.pre_read_code = (.hooks.pre_read_code // []) + [{"command": $cmd}] |
            .hooks.pre_write_code = (.hooks.pre_write_code // []) + [{"command": $cmd}]
        ' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"hooks":{
  "pre_run_command":[{"command":"$SAFEHOOKS_DIR/validator.py"}],
  "pre_read_code":[{"command":"$SAFEHOOKS_DIR/validator.py"}],
  "pre_write_code":[{"command":"$SAFEHOOKS_DIR/validator.py"}]
}}
EOF
    fi
    log_success "Windsurf configured"
    log_dim "Settings: $f"
    ((INSTALLED++))
}

uninstall_windsurf() {
    local f="$HOME/.codeium/windsurf/hooks.json"
    [ ! -f "$f" ] && return
    jq 'if .hooks then .hooks |= with_entries(
        .value = [.value[]? | select(.command | contains("safehooks") | not)]
    ) | .hooks |= with_entries(select(.value | length > 0)) else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "Windsurf: removed"
}

#=============================================================================
# GEMINI CLI
#=============================================================================
detect_gemini() { [ -d "$HOME/.gemini" ] || command -v gemini &>/dev/null; }

install_gemini() {
    local f="$HOME/.gemini/settings.json"
    mkdir -p "$HOME/.gemini"

    if [ -f "$f" ]; then
        jq -e '.hooks.BeforeTool[]? | select(.hooks[]?.command | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "Gemini CLI: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            .hooks = (.hooks // {}) |
            .hooks.BeforeTool = (.hooks.BeforeTool // []) + [{
                "matcher": "run_shell_command|write_file|read_file",
                "hooks": [{"type": "command", "command": $cmd, "timeout": 30000}]
            }]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"hooks":{"BeforeTool":[{
  "matcher":"run_shell_command|write_file|read_file",
  "hooks":[{"type":"command","command":"$SAFEHOOKS_DIR/validator.py","timeout":30000}]
}]}}
EOF
    fi
    log_success "Gemini CLI configured"
    log_dim "Settings: $f"
    ((INSTALLED++))
}

uninstall_gemini() {
    local f="$HOME/.gemini/settings.json"
    [ ! -f "$f" ] && return
    jq 'if .hooks.BeforeTool then .hooks.BeforeTool = [.hooks.BeforeTool[] |
        select(.hooks[]?.command | contains("safehooks") | not)] else . end |
        if .hooks.BeforeTool == [] then del(.hooks.BeforeTool) else . end |
        if .hooks == {} then del(.hooks) else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "Gemini CLI: removed"
}

#=============================================================================
# GITHUB COPILOT
#=============================================================================
detect_github_copilot() {
    [ -d "$HOME/.config/github-copilot" ] || command -v gh &>/dev/null
}

install_github_copilot() {
    # GitHub Copilot uses .github/hooks/ per-repo, but we can set up a global template
    local f="$HOME/.config/github-copilot/hooks.json"
    mkdir -p "$HOME/.config/github-copilot"

    if [ -f "$f" ]; then
        jq -e '.hooks.preToolUse[]? | select(.bash | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "GitHub Copilot: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            .hooks = (.hooks // {}) |
            .hooks.preToolUse = (.hooks.preToolUse // []) + [{
                "type": "command",
                "bash": $cmd,
                "timeoutSec": 30
            }]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"version":1,"hooks":{
  "preToolUse":[{"type":"command","bash":"$SAFEHOOKS_DIR/validator.py","timeoutSec":30}]
}}
EOF
    fi
    log_success "GitHub Copilot configured"
    log_dim "Settings: $f"
    log_dim "Note: Copy to .github/hooks/hooks.json in repos for full support"
    ((INSTALLED++))
}

uninstall_github_copilot() {
    local f="$HOME/.config/github-copilot/hooks.json"
    [ ! -f "$f" ] && return
    jq 'if .hooks.preToolUse then .hooks.preToolUse = [.hooks.preToolUse[] |
        select(.bash | contains("safehooks") | not)] else . end |
        if .hooks.preToolUse == [] then del(.hooks.preToolUse) else . end |
        if .hooks == {} then del(.hooks) else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "GitHub Copilot: removed"
}

#=============================================================================
# AMP CODE
#=============================================================================
detect_amp() { [ -d "$HOME/.amp" ] || command -v amp &>/dev/null; }

install_amp() {
    local f="$HOME/.amp/settings.json"
    mkdir -p "$HOME/.amp"

    if [ -f "$f" ]; then
        jq -e '."amp.hooks"[]? | select(.action.command | contains("safehooks"))' "$f" &>/dev/null && {
            log_dim "Amp Code: already configured"; return; }
        cp "$f" "$f.backup.$(date +%s)"
        jq --arg cmd "$SAFEHOOKS_DIR/validator.py" '
            ."amp.hooks" = (."amp.hooks" // []) + [{
                "id": "safehooks-validator",
                "on": {"event": "tool:pre-execute", "tool": ["bash", "write_file", "read_file"]},
                "action": {"type": "command", "command": $cmd}
            }]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    else
        cat > "$f" << EOF
{"amp.hooks":[{
  "id":"safehooks-validator",
  "on":{"event":"tool:pre-execute","tool":["bash","write_file","read_file"]},
  "action":{"type":"command","command":"$SAFEHOOKS_DIR/validator.py"}
}]}
EOF
    fi
    log_success "Amp Code configured"
    log_dim "Settings: $f"
    ((INSTALLED++))
}

uninstall_amp() {
    local f="$HOME/.amp/settings.json"
    [ ! -f "$f" ] && return
    jq 'if ."amp.hooks" then ."amp.hooks" = [."amp.hooks"[] |
        select(.action.command | contains("safehooks") | not)] else . end |
        if ."amp.hooks" == [] then del(."amp.hooks") else . end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    log_success "Amp Code: removed"
}

#=============================================================================
# OPENCODE
#=============================================================================
detect_opencode() { [ -d "$HOME/.config/opencode" ] || command -v opencode &>/dev/null; }

install_opencode() {
    # OpenCode uses plugins for hooks
    local pdir="$HOME/.config/opencode/plugins"
    local f="$pdir/safehooks.ts"
    mkdir -p "$pdir"

    [ -f "$f" ] && {
        log_dim "OpenCode: already configured"; return; }

    cat > "$f" << 'PLUGIN'
// SafeHooks plugin for OpenCode
import { definePlugin } from "opencode";

const BLOCK_PATTERNS = [
  { pattern: /rm\s+(-rf|-fr|--recursive)\s+[\/~]/, reason: "Recursive delete on root/home" },
  { pattern: /mkfs|dd\s+if=.*of=\/dev/, reason: "Disk destruction" },
  { pattern: /curl.*\|\s*(ba)?sh|wget.*\|\s*(ba)?sh/, reason: "Pipe to shell" },
  { pattern: /nc\s+-e|ncat\s+-e/, reason: "Reverse shell" },
];

export default definePlugin({
  name: "safehooks",
  before: async ({ tool, args }) => {
    if (tool === "bash" || tool === "exec") {
      const cmd = args?.command || "";
      for (const { pattern, reason } of BLOCK_PATTERNS) {
        if (pattern.test(cmd)) {
          throw new Error(`BLOCKED: ${reason}`);
        }
      }
    }
    if (tool === "read" || tool === "write") {
      const path = args?.file_path || "";
      if (/\.env($|\.)/.test(path) || /\.ssh\//.test(path)) {
        throw new Error("BLOCKED: Sensitive file access");
      }
    }
  }
});
PLUGIN
    log_success "OpenCode configured (plugin)"
    log_dim "Plugin: $f"
    ((INSTALLED++))
}

uninstall_opencode() {
    rm -f "$HOME/.config/opencode/plugins/safehooks.ts"
    log_success "OpenCode: removed"
}

#=============================================================================
# CODEX CLI (limited support)
#=============================================================================
detect_codex() { [ -d "$HOME/.codex" ] || command -v codex &>/dev/null; }

install_codex() {
    log_warn "Codex CLI: Limited support (no pre-tool hooks)"
    log_dim "Patterns available at ~/.safehooks/ for reference"
    ((INSTALLED++))
}

uninstall_codex() { log_dim "Codex CLI: No hooks to remove"; }

#=============================================================================
# MAIN
#=============================================================================

list_tools() {
    echo "Detected AI coding tools:"
    echo ""
    detect_claude         && echo -e "  ${GREEN}✓${NC} Claude Code/Desktop/Cowork"
    detect_cursor         && echo -e "  ${GREEN}✓${NC} Cursor"
    detect_windsurf       && echo -e "  ${GREEN}✓${NC} Windsurf"
    detect_gemini         && echo -e "  ${GREEN}✓${NC} Gemini CLI"
    detect_github_copilot && echo -e "  ${GREEN}✓${NC} GitHub Copilot"
    detect_amp            && echo -e "  ${GREEN}✓${NC} Amp Code"
    detect_opencode       && echo -e "  ${GREEN}✓${NC} OpenCode"
    detect_codex          && echo -e "  ${YELLOW}~${NC} Codex CLI (limited)"
    echo ""
    echo "Not detected:"
    detect_claude         || echo -e "  ${DIM}○ Claude Code${NC}"
    detect_cursor         || echo -e "  ${DIM}○ Cursor${NC}"
    detect_windsurf       || echo -e "  ${DIM}○ Windsurf${NC}"
    detect_gemini         || echo -e "  ${DIM}○ Gemini CLI${NC}"
    detect_github_copilot || echo -e "  ${DIM}○ GitHub Copilot${NC}"
    detect_amp            || echo -e "  ${DIM}○ Amp Code${NC}"
    detect_opencode       || echo -e "  ${DIM}○ OpenCode${NC}"
    detect_codex          || echo -e "  ${DIM}○ Codex CLI${NC}"
    echo -e "  ${DIM}○ VS Code Copilot Agent (hooks coming soon)${NC}"
}

install_all() {
    echo ""
    echo "  ╔═══════════════════════════════════════════╗"
    echo "  ║           SafeHooks v${VERSION}               ║"
    echo "  ║   Security rules for AI coding assistants ║"
    echo "  ╚═══════════════════════════════════════════╝"
    echo ""

    check_jq
    install_patterns
    install_validator
    echo ""
    log_info "Configuring detected tools..."
    echo ""

    if [ -n "$SPECIFIC_TOOL" ]; then
        case "$SPECIFIC_TOOL" in
            claude-code|claude|claude-desktop|claude-cowork) install_claude ;;
            cursor) install_cursor ;;
            windsurf|codeium) install_windsurf ;;
            gemini-cli|gemini) install_gemini ;;
            github-copilot|copilot) install_github_copilot ;;
            amp-code|amp) install_amp ;;
            opencode) install_opencode ;;
            codex-cli|codex) install_codex ;;
            *) log_error "Unknown: $SPECIFIC_TOOL"; exit 1 ;;
        esac
    else
        detect_claude         && install_claude
        detect_cursor         && install_cursor
        detect_windsurf       && install_windsurf
        detect_gemini         && install_gemini
        detect_github_copilot && install_github_copilot
        detect_amp            && install_amp
        detect_opencode       && install_opencode
        detect_codex          && install_codex
    fi

    echo ""
    [ $INSTALLED -eq 0 ] && {
        log_warn "No supported tools detected"
        echo "Install one: cursor.com, claude.ai/code, windsurf.com, geminicli.com"
        exit 1
    }

    log_success "SafeHooks installed for $INSTALLED tool(s)"
    echo ""
    echo "Patterns: ~/.safehooks/"
    echo "Update:   curl -fsSL ${REPO_RAW_URL}/install.sh | bash -s -- --update"
    echo "Remove:   curl -fsSL ${REPO_RAW_URL}/install.sh | bash -s -- --uninstall"
    echo ""
}

uninstall_all() {
    log_info "Removing SafeHooks..."
    echo ""
    check_jq
    uninstall_claude
    uninstall_cursor
    uninstall_windsurf
    uninstall_gemini
    uninstall_github_copilot
    uninstall_amp
    uninstall_opencode
    uninstall_codex
    [ -d "$SAFEHOOKS_DIR" ] && rm -rf "$SAFEHOOKS_DIR" && log_success "Removed ~/.safehooks/"
    echo ""
    log_success "SafeHooks uninstalled"
}

update_patterns() {
    log_info "Updating patterns..."
    check_jq
    install_patterns
    echo ""
    log_success "Patterns updated"
}

case "$ACTION" in
    install)   install_all ;;
    uninstall) uninstall_all ;;
    update)    update_patterns ;;
    list)      list_tools ;;
esac
