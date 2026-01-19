# Armour

> 🛡️ Security-enhanced MCP proxy for Claude Code - Block destructive tool calls and audit all operations.

## Installation

### One-line install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/fuushyn/armour/main/scripts/install-armour.sh | bash
```

This installs the marketplace, installs the plugin, and enables it by writing to `~/.claude/settings.json`. Restart Claude Code to load the plugin.
Use `ARMOUR_MARKETPLACE_SOURCE` to override the marketplace source (for example, `https://github.com/fuushyn/armour.git`).

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

## Acknowledgments

- Built with the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- Inspired by security proxies like Nginx, Envoy, and Kubernetes API gateway patterns
- Thanks to the Claude community for feedback and testing

## Support

- 📖 [Documentation](https://github.com/fuushyn/armour)
- 🐛 [Report Issues](https://github.com/fuushyn/armour/issues)
- 💬 [Discussions](https://github.com/fuushyn/armour/discussions)

---

**Made with ❤️ for AI safety**
