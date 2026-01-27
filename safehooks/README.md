# SafeHooks

Security rules for AI coding assistants. Blocks dangerous commands and protects sensitive files with a single curl command.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/anthropics/armour/main/safehooks/install.sh | bash
```

Auto-detects and configures all supported tools on your system.

## Supported Tools

| Tool | Hooks Support | Pre-Tool Event | Config Location |
|------|--------------|----------------|-----------------|
| [Claude Code](https://claude.ai/code) | Full | `PreToolUse` | `~/.claude/settings.json` |
| [Claude Desktop](https://claude.ai/download) | Full | `PreToolUse` | `~/.claude/settings.json` |
| [Claude Cowork](https://claude.ai) | Full | `PreToolUse` | `~/.claude/settings.json` |
| [Cursor](https://cursor.com) | Full | `preToolUse` + `beforeShellExecution` | `~/.cursor/hooks.json` |
| [Windsurf](https://windsurf.com) | Full | `pre_run_command` | `~/.codeium/windsurf/hooks.json` |
| [Gemini CLI](https://geminicli.com) | Full | `BeforeTool` | `~/.gemini/settings.json` |
| [GitHub Copilot](https://github.com/features/copilot) | Full | `preToolUse` | `.github/hooks/hooks.json` |
| [Amp Code](https://ampcode.com) | Full | `tool:pre-execute` | `~/.amp/settings.json` |
| [OpenCode](https://opencode.ai) | Plugin | `before` handler | `~/.config/opencode/plugins/` |
| [Codex CLI](https://github.com/openai/codex) | Limited | Notification only | - |
| VS Code Copilot Agent | Coming soon | [Feature request](https://github.com/microsoft/vscode/issues/264046) | - |

## Commands

```bash
# Install for all detected tools
curl -fsSL .../install.sh | bash

# Install for specific tool
curl -fsSL .../install.sh | bash -s -- --tool cursor
curl -fsSL .../install.sh | bash -s -- --tool windsurf
curl -fsSL .../install.sh | bash -s -- --tool claude-code

# Update patterns only
curl -fsSL .../install.sh | bash -s -- --update

# Uninstall from all tools
curl -fsSL .../install.sh | bash -s -- --uninstall

# List detected tools
curl -fsSL .../install.sh | bash -s -- --list
```

## What Gets Installed

```
~/.safehooks/
├── block-patterns.json   # Dangerous command patterns
├── allow-patterns.json   # Safe command patterns (auto-approved)
├── tool-rules.json       # Tool-level configuration
└── validator.py          # Universal hook script
```

Plus tool-specific hook configuration.

## What Gets Blocked

| Category | Examples | Level |
|----------|----------|-------|
| Critical deletion | `rm -rf /`, `rm -rf ~` | 10 |
| Disk destruction | `mkfs`, `dd if=... of=/dev/` | 10 |
| Privilege escalation | Write to `/etc/passwd`, `/etc/shadow` | 10 |
| Fork bombs | `:(){:\|:&};:` | 10 |
| Reverse shells | `nc -e`, `ncat -e` | 9 |
| Crypto miners | `xmrig`, `minerd`, `cgminer` | 9 |
| Remote code exec | `curl ... \| bash`, `wget ... \| sh` | 8 |
| Data exfiltration | `curl -d $(cat ...)` | 8 |
| Sensitive files | `.env`, `.ssh/`, `.aws/credentials` | - |

## What Gets Auto-Allowed

Common safe operations skip validation:

- **Read-only**: `ls`, `pwd`, `whoami`, `date`, `uptime`
- **Git read**: `git status`, `git log`, `git diff`, `git branch`
- **Package info**: `npm list`, `yarn outdated`, `pnpm view`
- **Version checks**: `node --version`, `python --version`

## Tool Configuration Details

### Claude Code / Desktop / Cowork

All Claude tools share the same configuration at `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "~/.safehooks/validator.py"}]},
      {"matcher": "Read|Write|Edit", "hooks": [{"type": "command", "command": "~/.safehooks/validator.py"}]}
    ]
  }
}
```

Docs: [Claude Code Hooks](https://code.claude.com/docs/en/hooks)

### Cursor

Cursor supports multiple hook types at `~/.cursor/hooks.json`:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [{"command": "~/.safehooks/validator.py", "timeout": 30}],
    "beforeShellExecution": [{"command": "~/.safehooks/validator.py", "timeout": 30}],
    "beforeReadFile": [{"command": "~/.safehooks/validator.py", "timeout": 30}]
  }
}
```

Docs: [Cursor Hooks](https://cursor.com/docs/agent/hooks) | [Deep Dive](https://blog.gitbutler.com/cursor-hooks-deep-dive)

### Windsurf (Codeium Cascade)

Configuration at `~/.codeium/windsurf/hooks.json`:

```json
{
  "hooks": {
    "pre_run_command": [{"command": "~/.safehooks/validator.py"}],
    "pre_read_code": [{"command": "~/.safehooks/validator.py"}],
    "pre_write_code": [{"command": "~/.safehooks/validator.py"}]
  }
}
```

Docs: [Windsurf Hooks](https://docs.windsurf.com/windsurf/cascade/hooks)

### Gemini CLI

Configuration at `~/.gemini/settings.json`:

```json
{
  "hooks": {
    "BeforeTool": [{
      "matcher": "run_shell_command|write_file|read_file",
      "hooks": [{"type": "command", "command": "~/.safehooks/validator.py", "timeout": 30000}]
    }]
  }
}
```

Docs: [Gemini CLI Hooks](https://geminicli.com/docs/hooks/)

### GitHub Copilot

Per-repo at `.github/hooks/hooks.json`:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [{"type": "command", "bash": "~/.safehooks/validator.py", "timeoutSec": 30}]
  }
}
```

Docs: [GitHub Copilot Hooks](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/use-hooks)

### Amp Code

Configuration at `~/.amp/settings.json`:

```json
{
  "amp.hooks": [{
    "id": "safehooks-validator",
    "on": {"event": "tool:pre-execute", "tool": ["bash", "write_file", "read_file"]},
    "action": {"type": "command", "command": "~/.safehooks/validator.py"}
  }]
}
```

Docs: [Amp Code Hooks](https://ampcode.com/news/hooks)

### OpenCode

Plugin at `~/.config/opencode/plugins/safehooks.ts`:

```typescript
import { definePlugin } from "opencode";

export default definePlugin({
  name: "safehooks",
  before: async ({ tool, args }) => {
    // Validation logic here
  }
});
```

Docs: [OpenCode Config](https://opencode.ai/docs/config/)

## Pattern Format

### Block Patterns (`block-patterns.json`)

```json
{
  "pattern": "rm\\s+(-rf|-fr)\\s+[\\/~]",
  "level": 10,
  "reason": "Recursive delete on root/home",
  "category": "critical_deletion",
  "tools": ["bash", "exec", "Bash"]
}
```

### Allow Patterns (`allow-patterns.json`)

```json
{
  "pattern": "^git\\s+(status|log|diff|branch)",
  "tools": ["bash", "exec", "Bash"]
}
```

## Exit Codes

| Code | Meaning | Behavior |
|------|---------|----------|
| 0 | Allow | Tool call proceeds |
| 2 | Block | Tool call blocked, error shown to AI |
| Other | Error | Tool call proceeds (fail-open) |

## Requirements

- `jq` - JSON processor (`brew install jq` / `apt install jq`)
- `python3` - For the validator script
- `curl` or `wget` - For downloading patterns

## Resources

- [Claude Code Hooks Guide](https://code.claude.com/docs/en/hooks-guide)
- [Cursor Hooks Deep Dive](https://blog.gitbutler.com/cursor-hooks-deep-dive)
- [Windsurf Hooks](https://docs.windsurf.com/windsurf/cascade/hooks)
- [Gemini CLI Hooks](https://geminicli.com/docs/hooks/)
- [GitHub Copilot Hooks](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/use-hooks)
- [Amp Code Hooks](https://ampcode.com/news/hooks)
- [OpenCode Config](https://opencode.ai/docs/config/)

## License

MIT
