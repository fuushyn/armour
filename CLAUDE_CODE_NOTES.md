# Claude Code CLI Research Notes

Sources (docs):
- https://code.claude.com/docs/en/cli-reference
- https://code.claude.com/docs/en/settings
- https://code.claude.com/docs/en/interactive-mode
- https://code.claude.com/docs/en/slash-commands
- https://code.claude.com/docs/en/headless
- https://code.claude.com/docs/en/hooks
- https://code.claude.com/docs/en/plugins
- https://code.claude.com/docs/en/plugins-reference
- https://code.claude.com/docs/en/skills
- https://code.claude.com/docs/en/mcp
- https://code.claude.com/docs/en/memory
- https://code.claude.com/docs/en/sub-agents
- https://code.claude.com/docs/en/output-styles
- https://code.claude.com/docs/en/statusline
- https://code.claude.com/docs/en/sandboxing
- https://code.claude.com/docs/en/model-config
- https://code.claude.com/docs/en/network-config
- https://code.claude.com/docs/en/setup
- https://code.claude.com/docs/en/common-workflows
- https://code.claude.com/docs/en/checkpointing

---

## Core CLI modes
- Interactive REPL: `claude` (optionally with initial prompt), supports slash commands, approvals, hooks, backgrounding.
- Print/headless mode: `claude -p "prompt"` runs agent loop via Agent SDK, then exits. Supports `--output-format` and structured output with `--json-schema`.
- Piped input: `cat file | claude -p "prompt"` uses stdin as extra context.
- Session continuity: `--continue/-c` (most recent in cwd), `--resume/-r` (by id/name or picker), `--fork-session` to resume but create new ID.
- Docs mention `claude commit` (quickstart) but it is not listed in CLI reference; verify in the binary.
- CLI help (v2.1.2) notes `--no-session-persistence` (print mode only) to disable saving sessions.

## CLI commands & flags (selected)
- `claude`, `claude "query"` (start REPL, optional initial prompt).
- `claude update` (self-update), `claude mcp` (MCP server management).
- `claude -p` (print mode), `--output-format text|json|stream-json`, `--input-format text|stream-json`.
- `--include-partial-messages` (stream-json only).
- `--json-schema` (print mode; returns `structured_output` in json format).
- `--max-turns` (print mode; error on limit).
- `--max-budget-usd` (print mode budget cap).
- `--model` (alias or full name).
- `--agent` (override agent), `--agents` (dynamic subagents JSON).
- `--tools` (restrict tools), `--allowedTools` (auto-approve), `--disallowedTools` (remove tools).
- `--permission-mode` (default|acceptEdits|dontAsk|bypassPermissions|plan); CLI help also shows `delegate` mode.
- `--settings` (settings JSON path/string), `--setting-sources` (user,project,local).
- `--system-prompt`, `--system-prompt-file` (print only), `--append-system-prompt`.
  - `--system-prompt` and `--system-prompt-file` are mutually exclusive; docs recommend `--append-system-prompt` for most cases.
- `--mcp-config`, `--strict-mcp-config` (MCP server config control).
- `--plugin-dir` (load plugins for session), `--ide` (auto-connect IDE).
- `--chrome` / `--no-chrome` (browser integration).
- `--debug` (category filters), `--verbose` (turn-by-turn logs).
- `--dangerously-skip-permissions` (skip prompts).
- Other notable flags: `--add-dir`, `--session-id`, `--permission-prompt-tool`, `--fallback-model` (print mode only).
- Other CLI-only flags (from `claude --help`): `--allow-dangerously-skip-permissions`, `--disable-slash-commands`,
  `--no-session-persistence`, `--replay-user-messages`.

## CLI subcommands (from `claude --help`)
- `doctor`, `install`, `mcp`, `plugin`, `setup-token`, `update`.

## Sessions & checkpointing
- Sessions stored per repo; picker via `/resume` or `claude --resume`.
- Session naming: `/rename`, resume by name.
- Checkpointing: auto snapshots before each edit tool use; rewind via `Esc Esc` or `/rewind`.
- Rewind can restore conversation only, code only, or both.
- Limitations: bash edits not tracked, external edits not tracked; not a replacement for git.
- Observed (v2.1.2): session transcripts are JSONL at `~/.claude/projects/<slug>/<session-id>.jsonl`.
  Hook payloads and transcript share the same `session_id`.

## Permissions, tools, and modes
- Tool set includes `Read`, `Edit`, `Write`, `Bash`, `Glob`, `Grep`, `WebFetch`, `WebSearch`, `Task` (subagents), `Skill`, etc.
- Permission rules: `allow`, `ask`, `deny`. Bash patterns are prefix matches, not regex.
- Default permission modes: `default`, `acceptEdits`, `dontAsk`, `bypassPermissions`, `plan`.
- `permissions.deny` doubles as file hiding (sensitive files are invisible).

## Hooks (event-driven automation)
- Events: `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Notification`, `UserPromptSubmit`,
  `Stop`, `SubagentStop`, `PreCompact`, `SessionStart`, `SessionEnd`, plus plugin-specific
  `SubagentStart` and `PostToolUseFailure`.
- Hook config format: event -> matcher -> hooks array. Matchers support regex or `*`.
- Hook types: `command` (bash), `prompt` (LLM decision), plugin-only `agent` verifier.
- Input: JSON via stdin with `tool_input`, `tool_name`, `session_id`, `cwd`, `permission_mode`, etc.
- Output: exit codes (0 ok, 2 block), or JSON with decision control (allow/deny/ask).
- SessionStart has `CLAUDE_ENV_FILE` to persist env vars for later bash calls.
- Hooks can be defined in settings, plugins, skills, subagents, and slash commands; scoped hooks auto-clean up.
- Enterprise setting `allowManagedHooksOnly` blocks user/project/plugin hooks.
- Custom triggers: no user-defined event types; use matchers + hook scripts to create “virtual events”
  (e.g., gate on `tool_input.command` patterns, notification_type, or prompt content).
- Scoped triggers: add hooks in slash command frontmatter to run only for that command; skills and subagents
  can also define hooks that apply only while they run (`once: true` supported for skills/commands).
- Override behavior: hooks run in parallel and don’t have deterministic ordering, so “override” is done via:
  - PreToolUse / PermissionRequest decisions (`allow`, `deny`, `ask`, `updatedInput`) to block or rewrite calls.
  - Stop / SubagentStop `decision: block` to force the agent to continue.
  - Project/user disable via `disableAllHooks` or managed-only `allowManagedHooksOnly`.
- Observed (v2.1.2) hook input shapes:
  - SessionStart: `{hook_event_name, session_id, source, cwd, transcript_path}`.
  - UserPromptSubmit: adds `{prompt, permission_mode}`.
  - PreToolUse: adds `{tool_name, tool_input, tool_use_id}`.
  - PostToolUse: adds `{tool_response}` (Bash includes stdout/stderr).
  - Stop: adds `{stop_hook_active}`.

## Context & transcripts (observed)
- Transcript entries are JSONL objects with `type` values like:
  - `queue-operation` (contains `operation`, `sessionId`, `timestamp`).
  - `user` / `assistant` (contain `message`, `cwd`, `gitBranch`, `sessionId`, `uuid`, `version`, `timestamp`).
  - Tool results show up as `user` entries with `toolUseResult`.
- Example `toolUseResult` for a Read call includes:
  - `type: "text"`
  - `file: { filePath, content, numLines, startLine, totalLines }`

## Settings & scopes
- Scopes: managed > CLI args > local > project > user.
- Files: `~/.claude/settings.json`, `.claude/settings.json`, `.claude/settings.local.json`.
- Managed: `managed-settings.json` in system directories; same for `managed-mcp.json`.
- State file: `~/.claude.json` holds OAuth, MCP configs, caches, project state.
- `CLAUDE_CONFIG_DIR` can relocate config storage.

## MCP (Model Context Protocol)
- Add servers: `claude mcp add --transport http|sse|stdio ...`; stdio uses `--` delimiter.
- Scopes: local (default), project (`.mcp.json`), user (`~/.claude.json`).
- Env var expansion in `.mcp.json`: `${VAR}` or `${VAR:-default}`.
- Dynamic updates: MCP `list_changed` refreshes tools/prompts/resources.
- Permissions: MCP tools named `mcp__server__tool`; allow by `mcp__server` or `mcp__server__*`.
- Resources: `@server:protocol://path` in prompts; prompts exposed as `/mcp__server__prompt`.
- Output limits: warning at 10k tokens; `MAX_MCP_OUTPUT_TOKENS` default 25k.
- Managed control: `managed-mcp.json` for exclusive control, or `allowedMcpServers` / `deniedMcpServers`.
- Claude Code can act as MCP server: `claude mcp serve`.

## Plugins
- Manifest: `.claude-plugin/plugin.json` with `name`, `version`, `description`, etc.
- Components: `commands/`, `agents/`, `skills/`, `hooks/`, `.mcp.json`, `.lsp.json`, `outputStyles`.
- Plugin cache: plugins copied to cache; paths must be relative (`./`) and stay within plugin root.
- `CLAUDE_PLUGIN_ROOT` env var for hooks/MCP/LSP scripts.
- CLI management: `claude plugin install|uninstall|enable|disable|update`.
- Scopes: user, project, local, managed.

## Skills
- `SKILL.md` with frontmatter (`name`, `description`, `allowed-tools`, `model`, `context: fork`, `hooks`, etc).
- Discovery: only name + description loaded at startup; full SKILL.md loaded on use.
- Locations: `~/.claude/skills/`, `.claude/skills/`, plugin skills; scope precedence applies.
- `context: fork` runs in subagent context; `agent:` selects subagent type.
- `user-invocable` toggles slash menu visibility; `disable-model-invocation` blocks Skill tool use.
- Subagents do not inherit skills unless listed in subagent `skills` field (full content injected).

## Subagents
- Built-ins: Explore (Haiku, read-only), Plan (read-only for plan mode), general-purpose.
- Custom agents: `.claude/agents/`, `~/.claude/agents/`, or `--agents` JSON.
- Frontmatter: `name`, `description`, `tools`, `disallowedTools`, `model`, `permissionMode`, `skills`, `hooks`.
- Background subagents auto-deny unapproved tools and cannot use MCP tools.
- Deny specific subagents with permission rule `Task(agent-name)`.

## Slash commands
- Built-ins include `/config`, `/permissions`, `/hooks`, `/mcp`, `/plugin`, `/rewind`, `/sandbox`, etc.
- Custom commands: `.claude/commands/` (project) and `~/.claude/commands/` (user).
- Argument placeholders: `$ARGUMENTS`, `$1`, `$2`, etc.
- Frontmatter: `allowed-tools`, `argument-hint`, `context: fork`, `agent`, `model`, `hooks`.
- Commands can include `!` bash snippets and `@` file references.
- Skill tool now invokes commands/skills programmatically (budget via `SLASH_COMMAND_TOOL_CHAR_BUDGET`).

## Interactive UX notes
- `Ctrl+B` backgrounds a running bash/tool task.
- `!` prefix runs bash directly and injects output into context.
- `Esc Esc` opens rewind; `Shift+Tab` cycles permission modes.
- `Option+T` toggles extended thinking; `Option+P` toggles model.

## Sandboxing
- OS-level sandbox (Seatbelt on macOS, bubblewrap on Linux) for bash tool.
- Filesystem isolation: read/write scoped to cwd; write outside requires permissions.
- Network isolation: proxy allowlist; requests outside allowed domains prompt user.
- Escape hatch: `dangerouslyDisableSandbox` fallback unless `allowUnsandboxedCommands=false`.
- Config via `sandbox` settings (auto-allow, excludedCommands, allowUnixSockets, proxy ports).
- Open-source runtime: `npx @anthropic-ai/sandbox-runtime <cmd>`.

## Output styles
- Output styles alter system prompt; built-ins: Default, Explanatory, Learning.
- Custom styles in `.claude/output-styles` or `~/.claude/output-styles` with frontmatter.
- `keep-coding-instructions` retains coding guidance in system prompt.

## Status line
- Configured via `statusLine` in settings or `/statusline`.
- Command receives JSON (model, cwd, cost, context window, etc) via stdin.
- Output is first line of stdout; updates ~every 300ms; ANSI colors allowed.

## Model config
- Aliases: `sonnet`, `opus`, `haiku`, `opusplan`, `sonnet[1m]`.
- `opusplan` uses Opus in plan mode then Sonnet for execution.
- Env vars map aliases: `ANTHROPIC_DEFAULT_*` plus `CLAUDE_CODE_SUBAGENT_MODEL`.
- Prompt caching toggles via `DISABLE_PROMPT_CACHING*`.

## Installation & updates
- Install script (`claude.ai/install.sh`), Homebrew, WinGet.
- Auto-updates via `autoUpdatesChannel` (latest|stable); `claude update` manual.
- `DISABLE_AUTOUPDATER` to opt out.

## Misc config notes
- `cleanupPeriodDays` controls session retention (0 deletes immediately).
- `fileSuggestion` can point to a custom command for `@` autocomplete.
- `CLAUDE_ENV_FILE` supports persisting env for bash tool sessions.
