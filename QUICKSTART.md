# Quick Start Guide

Get MCP Go Proxy running in **2 minutes**.

## As a Claude Code Plugin (Recommended)

### Step 1: Install
```
/plugin install mcp-go-proxy
```

### Step 2: Setup
```
/proxy-setup
```

The wizard will:
1. Auto-detect your MCP servers
2. Ask you to select which ones to proxy
3. Let you choose a security policy
4. Save the configuration

### Step 3: Done!
Your proxy is now live. All tools from your MCP servers will appear in Claude Code with namespace prefixes:
- `github:create_issue`
- `filesystem:read_file`
- `slack:send_message`

**That's it!** You're now using a security-enhanced MCP proxy.

---

## As a Standalone Binary

### Step 1: Build
```bash
git clone https://github.com/yourusername/mcp-go-proxy
cd mcp-go-proxy
go build -o mcp-proxy .
```

### Step 2: Configure
Create `servers.json`:
```json
{
  "servers": [
    {
      "name": "github",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-github"]
    }
  ],
  "policy": {
    "mode": "moderate"
  }
}
```

### Step 3: Run
```bash
# As stdio MCP server (for Claude Code)
./mcp-proxy -mode stdio -config servers.json

# Or as HTTP proxy on port 8080
./mcp-proxy -mode http -config servers.json -listen :8080
```

---

## Common Commands

### Detect existing MCP servers
```bash
./mcp-proxy detect
```

Output:
```
Found 3 MCP server(s):

1. github (stdio)
   Command: npx @modelcontextprotocol/server-github
   Source: ~/.claude/.mcp.json

2. filesystem (http)
   URL: http://localhost:3000
   Source: .mcp.json

3. slack (stdio)
   Command: npx @modelcontextprotocol/server-slack
   Source: ~/.claude/.mcp.json
```

### Auto-discover project servers
```bash
./mcp-proxy up
```

Scans your project for:
- `package.json` (NPM MCP packages)
- `docker-compose.yml` (Docker services)
- `pyproject.toml` (Python packages)
- `go.mod` (Go modules)

### Get help
```bash
./mcp-proxy help
```

---

## Configuration Locations

### User-Level Config
`~/.claude/mcp-proxy/servers.json`

Applies to all Claude Code instances. Typical contents:
```json
{
  "servers": [
    {"name": "github", "transport": "stdio", ...},
    {"name": "filesystem", "transport": "http", ...}
  ],
  "policy": {"mode": "moderate"}
}
```

### Policy Modes

| Mode | Use Case | Blocks |
|------|----------|--------|
| **Strict** | Testing untrusted servers | Everything except reads |
| **Moderate** | Development (recommended) | Destructive patterns (rm*, delete*) |
| **Permissive** | Trusted environments | Nothing |

---

## Security Policies Explained

### Strict Mode 🔒
Maximum protection. Use when:
- Testing new MCP servers
- Running in production
- High-security requirements

Blocks:
- `rm*` - file removal commands
- `delete*` - database/API deletion
- `drop*` - database schema modification
- Sampling and elicitation features
- Any experimental features

### Moderate Mode ⚖️ (Recommended)
Balanced security. Use when:
- Developing locally
- Using trusted servers
- Want good security without friction

Blocks:
- Obviously destructive patterns
- `rm*`, `delete*`, `drop*`, `truncate*`

Allows:
- Most operations
- Sampling and elicitation
- Read/write operations

### Permissive Mode ⚠️
No restrictions. Use when:
- You trust all servers completely
- Running sandboxed environment
- Need full feature access

Blocks:
- Nothing

---

## Troubleshooting

### "No servers detected"
The wizard couldn't find your MCP servers. Add them manually to `~/.claude/mcp-proxy/servers.json`:

```json
{
  "servers": [
    {
      "name": "my-server",
      "transport": "stdio",
      "command": "npx",
      "args": ["@my-org/my-mcp-server"]
    }
  ]
}
```

Then restart Claude Code.

### "Tools don't appear in Claude Code"
1. Make sure servers are in `~/.claude/mcp-proxy/servers.json`
2. Restart Claude Code
3. Check proxy logs: `MCP_PROXY_LOG_LEVEL=debug`

### "Proxy blocks a tool I need"
Change the policy mode to less restrictive:

```json
{
  "policy": {
    "mode": "permissive"
  }
}
```

Or update your Claude Code settings and restart.

---

## Web Dashboard

Access the real-time dashboard at:
```
http://localhost:9090
```

Shows:
- 📊 Statistics (blocked calls, allowed calls, block rate)
- 🖥️ Server status
- 🔐 Current policy mode
- 📝 (coming soon) Audit logs

---

## Next Steps

1. ✅ Install the plugin or build the binary
2. ✅ Run the setup wizard or configure `servers.json`
3. ✅ Choose a policy mode
4. ✅ Start using tools through the proxy
5. 📊 Monitor stats on the web dashboard

---

## Examples

### Example 1: GitHub + Filesystem Servers
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
      "url": "http://localhost:3000"
    }
  ],
  "policy": {"mode": "moderate"}
}
```

Now you can:
- `github:create_issue` - via GitHub server
- `filesystem:read_file` - via filesystem server
- `filesystem:write_file` - allowed in moderate mode

### Example 2: Strict Mode for Production
```json
{
  "servers": [
    {
      "name": "database",
      "transport": "http",
      "url": "http://postgres-mcp:3000"
    }
  ],
  "policy": {"mode": "strict"}
}
```

With strict mode:
- Read queries: ✅ Allowed
- Write queries: ✅ Allowed
- `DROP TABLE`: ❌ Blocked
- `DELETE FROM`: ❌ Blocked
- Sampling: ❌ Blocked

---

## Getting Help

- 📖 [Full Documentation](README.md)
- 🐛 [Report Issues](https://github.com/yourusername/mcp-go-proxy/issues)
- 💬 [Ask Questions](https://github.com/yourusername/mcp-go-proxy/discussions)

---

Happy proxying! 🚀
