# SafeHooks Config

Configuration for the SafeHooks threat detection service.

## Files

- `block-patterns.json` - Patterns that instantly block (no LLM call)
- `allow-patterns.json` - Patterns that instantly allow (no LLM call)
- `tool-rules.json` - Tool-level block/allow lists and thresholds
- `prompt.txt` - LLM prompt template for unknown patterns

## Pattern Format

### Block Patterns
```json
{
  "pattern": "regex-string",
  "level": 1-10,
  "reason": "Why this is blocked",
  "category": "category_name",
  "tools": ["bash", "exec"]  // optional - if omitted, applies to all tools
}
```

### Allow Patterns
```json
{
  "pattern": "regex-string",
  "tools": ["bash", "exec"]  // optional - if omitted, applies to all tools
}
```

### Tool Rules
```json
{
  "blocked_tools": ["dangerous_tool"],      // Always block these tools
  "allowed_tools": ["safe_tool"],           // Always allow these tools (skip all checks)
  "tool_thresholds": {                      // Per-tool threat thresholds
    "Write": 5,                             // Block Write if level >= 5
    "Bash": 8                               // Block Bash if level >= 8
  },
  "tool_patterns": {                        // Per-tool argument patterns
    "exec": {
      "block": [
        { "pattern": "rm\\s", "level": 8, "reason": "rm in exec", "category": "deletion" }
      ],
      "allow": [
        { "pattern": "^echo\\s" }
      ]
    }
  }
}
```

## Evaluation Order

1. Check `blocked_tools` → instant block
2. Check `allowed_tools` → instant allow
3. Check block patterns (filtered by tool if specified)
4. Check allow patterns (filtered by tool if specified)
5. LLM analysis with tool-specific threshold (or global default)

## Deployment

On push to `main` that modifies files in `safehooks/`, the GitHub Action automatically pushes config to Cloudflare KV.

## Manual Deploy

```bash
export KV_ID="07be0098d9ea48669799b36c9549969e"
wrangler kv key put "config:block-patterns" "$(cat block-patterns.json)" --namespace-id $KV_ID --remote
wrangler kv key put "config:allow-patterns" "$(cat allow-patterns.json)" --namespace-id $KV_ID --remote
wrangler kv key put "config:tool-rules" "$(cat tool-rules.json)" --namespace-id $KV_ID --remote
wrangler kv key put "config:llm-prompt" "$(cat prompt.txt)" --namespace-id $KV_ID --remote
wrangler kv key put "config:version" "manual-$(date +%Y%m%d)" --namespace-id $KV_ID --remote
```
