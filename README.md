# PreHooks.ai

> 🛡️ The Firewall for AI Agents - Block dangerous commands before they execute.

```bash
curl -fsSL prehooks.ai/install.sh | bash
```

## What it does

PreHooks intercepts tool calls from AI coding assistants and blocks dangerous operations:

- `rm -rf /` → **BLOCKED**
- `curl ... | bash` → **BLOCKED**
- SSH key exfiltration → **BLOCKED**
- Crypto miners → **BLOCKED**

## Supported Tools

- Claude Code / Claude Desktop / Claude Cowork
- Cursor
- Windsurf
- Gemini CLI
- GitHub Copilot
- Amp Code
- OpenCode

## How it works

1. **Pattern matching** - Fast regex-based blocking for known threats (~5ms)
2. **LLM analysis** - Cerebras-powered analysis for ambiguous cases (~100ms)
3. **Logging** - All tool calls logged for audit

## Links

- 🌐 [prehooks.ai](https://prehooks.ai) - Landing page
- 📖 [Documentation](https://prehooks.ai)
- 🐛 [Report Issues](https://github.com/fuushyn/armour/issues)

---

# Armour (MCP Proxy)

> Security-enhanced MCP proxy for Claude Code - Block destructive tool calls and audit all operations.

## Installation

### One-line install (binary bundle)

```bash
curl -fsSL https://raw.githubusercontent.com/fuushyn/armour/main/scripts/install-armour.sh | bash
```

This downloads a prebuilt plugin bundle, installs the marketplace locally, installs the plugin, and enables it by writing to `~/.claude/settings.json`.
Restart Claude Code to load the plugin.

Overrides:
```
ARMOUR_RELEASE_TAG=v1.0.3
ARMOUR_RELEASE_URL=https://github.com/fuushyn/armour/releases/download/v1.0.3/armour-plugin-darwin-arm64.tar.gz
ARMOUR_INSTALL_DIR=~/.armour/armour-plugin
```

Use `ARMOUR_MARKETPLACE_SOURCE` to fall back to a Git marketplace (for example, `https://github.com/fuushyn/armour.git`).

### Manual install

1. **Add marketplace** in Claude Code:
   ```
   /plugin add-marketplace fuushyn/armour
   ```

2. **Install plugin**:
   ```
   /plugin install armour
   ```

3. **Restart Claude Code** to load the plugin

4. **View MCP tools**:
   ```
   /mcp
   ```

That's it! All your MCP servers now route through Armour with security policies applied.

## Features

- **🔒 Security Policies**: Choose from strict, moderate, or permissive policies
- **📊 Multi-Server Support**: Aggregate multiple MCP servers with automatic namespacing
- **🎯 Audit Logging**: Complete audit trail of all tool calls
- **⚡ Zero Configuration**: Automatic server detection and configuration

## Configuration

Servers are configured in `~/.armour/servers.json` and automatically synced on each session start.

## Security Policies

- **Strict**: Blocks rm*, delete*, drop*, and sampling attempts. Read-only mode.
- **Moderate**: Blocks destructive operations but allows most normal operations.
- **Permissive**: No blocking, audit mode only.

## License

MIT License - see [LICENSE](LICENSE) file for details.

---

**Made with ❤️ for AI safety**
