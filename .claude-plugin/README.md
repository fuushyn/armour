# Armour Plugin

A Claude Code plugin that provides security-enhanced MCP proxy with centralized policy controls.

## What It Does

Armour helps you safely use multiple MCP servers with Claude by:

1. **Aggregating multiple MCP servers** into a single secure endpoint
2. **Blocking destructive tool calls** based on configurable policies
3. **Auditing all operations** for compliance and debugging
4. **Providing policy modes** for different security needs

## Installation

```bash
/plugin install armour
```

## Quick Start

### Automatic Server Registration (SessionStart Hook)

When you install this plugin, a **SessionStart hook** is automatically enabled that:

1. **Detects new servers** added via `claude mcp add` command
2. **Syncs them automatically** to the proxy registry on each session start
3. **Removes duplicates** from your local project config

This means you can use the standard Claude Code workflow:
```bash
claude mcp add --transport http context7 https://mcp.context7.com/mcp
```

The server will automatically be proxied through Armour on the next session start. No additional configuration needed!

### Option 1: Using the Built-in Tools (Recommended)

After installation, use the built-in MCP tools available in Claude Code:

1. **Detect Servers**: Use the `proxy:detect-servers` tool to find your existing MCP servers
2. **Check Status**: Use the `proxy:server-status` tool to see the proxy status
3. **Configure**: Edit `~/.armour/servers.json` to add servers

### Option 2: Interactive Setup Wizard (Terminal)

Run the setup wizard from the terminal:

```bash
~/.claude-plugin/scripts/setup-wizard.js
```

This will:
- Auto-detect your existing MCP servers
- Let you select which to proxy
- Choose your security policy (strict, moderate, permissive)
- Save configuration and activate the proxy

**That's it!** Restart Claude Code and your proxy is now live.

## Policy Modes

### Strict Mode
**Maximum security.** Use when you're testing untrusted MCP servers or running in production.

- ✓ Allows: Read operations, safe resource access
- ✗ Blocks: Sampling, elicitation, destructive tools (rm*, delete*, drop*, etc.)
- 📊 Audit: All operations logged

### Moderate Mode (Recommended)
**Balanced security.** Good for most development workflows.

- ✓ Allows: Most operations
- ✗ Blocks: Obvious destructive tools (rm*, delete*, drop*, truncate*, etc.)
- 📊 Audit: Sensitive operations logged

### Permissive Mode
**Minimal restrictions.** Use with caution - for trusted environments only.

- ✓ Allows: Everything
- ✗ Blocks: Nothing
- 📊 Audit: Minimal logging

## Configuration

Configuration is stored in `~/.armour/servers.json` with this structure:

```json
{
  "servers": [
    {
      "name": "github",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-github"]
    },
    {
      "name": "filesystem",
      "transport": "http",
      "url": "http://localhost:8080"
    }
  ],
  "policy": {
    "mode": "moderate"
  }
}
```

## Advanced: Manual Configuration

To manually add servers, edit `~/.armour/servers.json` and restart Claude Code.

### For stdio servers (processes):
```json
{
  "name": "my-server",
  "transport": "stdio",
  "command": "npx",
  "args": ["@my-org/my-mcp-server"]
}
```

### For HTTP servers:
```json
{
  "name": "my-server",
  "transport": "http",
  "url": "http://localhost:9000"
}
```

## Troubleshooting

### SessionStart Hook Not Working

The hook automatically syncs servers added via `claude mcp add` to the proxy registry. If servers aren't being synced:

1. **Verify the hook is installed** - Check that `~/.claude-plugin/hooks/hooks.json` exists
2. **Check hook permissions** - The script should be executable:
   ```bash
   ls -la ~/.claude-plugin/hooks/sync-mcp-servers.sh
   # Should show: -rwxr-xr-x (or similar with execute permission)
   ```
3. **Manual sync** - You can manually run the hook:
   ```bash
   bash ~/.claude-plugin/hooks/sync-mcp-servers.sh
   ```
4. **Verify registry** - Check that servers appear in `~/.armour/servers.json`

### Proxy doesn't start
- Check that `~/.armour/servers.json` exists and is valid JSON
- Check Claude Code logs for errors
- Try running with stricter policy: `ARMOUR_POLICY=strict`

### Tools not appearing
- Make sure servers are configured in `servers.json`
- Restart Claude Code after adding new servers
- Check that tool names don't conflict (they're namespaced as `server:tool`)
- Verify HTTP servers have proper headers (e.g., API keys) in registry

### Need to reset
Delete the configuration:
```bash
rm -rf ~/.armour/
```

Then restart Claude Code to reinitialize.

## How Tool Names Work

Tools are automatically namespaced to prevent conflicts:

```
github:create_issue
github:list_repos
filesystem:read_file
filesystem:write_file
```

This prevents tools from different servers with the same name from colliding.

## Security Considerations

- **The proxy validates all policy rules before forwarding to servers**
- **Session IDs isolate traffic between different Claude Code instances**
- **Audit logs track all operations** (check `~/.armour/audit.db`)
- **Environment variables** are preserved from your original server configs

## Feedback & Contributing

Found a bug? Have a feature request? Visit:
https://github.com/fuushyn/armour/issues

## License

MIT
