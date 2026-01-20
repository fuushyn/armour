# Armour

> 🛡️ Security-enhanced MCP proxy for Claude Code - Block destructive tool calls and audit all operations.

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

## Release steps

1. Bump versions:
   ```bash
   VERSION="v1.0.6"
   sed -i '' "s/\"version\": \"[^\"]*\"/\"version\": \"${VERSION#v}\"/" .claude-plugin/plugin.json
   sed -i '' "s/\"version\": \"[^\"]*\"/\"version\": \"${VERSION#v}\"/" .claude-plugin/marketplace.json
   ```
2. Build the bundle:
   ```bash
   scripts/build-plugin-bundle.sh
   ```
3. Commit and push:
   ```bash
   git add .claude-plugin/plugin.json .claude-plugin/marketplace.json
   git commit -m "Release ${VERSION}"
   git push
   ```
4. Create the GitHub release:
   ```bash
   gh release create "${VERSION}" "dist/armour-plugin-$(go env GOOS)-$(go env GOARCH).tar.gz" \
     -t "${VERSION}" \
     -n "- Release ${VERSION}"
   ```
5. Smoke-test the curl installer:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/fuushyn/armour/main/scripts/install-armour.sh | bash
   ```
   Restart Claude Code.

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
- 📚 [Claude Code LLMs](https://code.claude.com/docs/llms.txt)
- 📚 [MCP LLMs](https://modelcontextprotocol.io/llms.txt)

---

**Made with ❤️ for AI safety**
