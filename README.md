# lathe

> A coding-agent CLI for the terminal: run an LLM in a tool-use loop against
> your workspace, with an interactive TUI and a non-interactive print mode.
> Built in Go on top of
> [agentscope-go](https://github.com/alanfokco/agentscope-go), in the mold of
> OpenAI Codex CLI and Anthropic Claude Code.

## Status

Pre-release, work in progress. The M1–M5b milestones are implemented and
covered by hermetic tests:

- **M1** turn engine + print mode · **M2** bubbletea TUI · **M3** dynamic
  system prompt / cost line / `/model` `/config` / auto-compact + `/compact` /
  JSONL sessions + `--resume`/`--continue` / interactive approval
- **M4** skills / MCP / hooks / Task subagent / sandbox
- **M5a** Ollama provider · **M5b** configurable status line

Not yet released; `agentscope-go` is currently consumed from a local checkout
(see [Build & run](#build--run)).

## Features

- **Two modes** — interactive bubbletea TUI (streaming text, tool calls, diffs,
  scrollback, slash commands, interactive y/n/a approval) and non-interactive
  `lathe -p "..."` print mode (`--output text` or `--output stream-json` NDJSON).
- **Self-built turn engine** — drives `model.ChatStream` directly (not a wrapper
  around `UnifiedAgent`): ChatStream → accumulate → emit text/usage → append
  assistant message → dispatch tool calls → feed results back as a user-role
  message → repeat until `end_turn` or `--max-iters`.
- **Cross-provider** — Anthropic, OpenAI, DashScope, and Ollama (local,
  OpenAI-compatible; also works with lmstudio/vllm via `--base-url`).
- **Tools & permissions** — Bash/Read/Write/Edit/Glob/Grep with a permission
  engine (`default` · `accept_edits` · `explore` · `bypass` · `dont_ask`) and
  interactive approval in the TUI; `always` upgrades a tool to auto-allow.
- **Optional sandbox** — `--sandbox docker` (container mounting cwd) or `e2b`
  (cloud); sandbox setup failure fails loudly — no silent fallback to host.
- **Sessions** — JSONL transcripts under `~/.lathe/projects/<enc-cwd>/<id>.jsonl`
  (claude-code-style project dirs); `--resume <id>` / `--continue`.
- **Auto-compact** — conversation summarized past `0.8 × context_size` (a
  `0.1 × ctx` tail reserved, tool_call/result pairs kept together); `/compact`
  to force.
- **Skills** — `SKILL.md` instruction docs discovered from `~/.lathe/skills` +
  project `.lathe/skills` (walked cwd→repo-root); exposed via a read-only
  `Skill` tool.
- **MCP** — `.mcp.json` `stdio`/`http` servers become ordinary toolkit tools.
- **Hooks** — `settings.json` shell-command hooks at `UserPromptSubmit` /
  `PreToolUse` / `PostToolUse` / `Stop`; a hook may block a tool or inject
  context. Failures are non-blocking.
- **Task subagent** — a `Task` tool that spawns a nested builtins-only engine
  (no `Task` → no recursion), sharing the parent's model + permission engine.
- **Configurable status line** — a `statusLine` shell command in `settings.json`
  receives a Claude-Code-compatible JSON snapshot on stdin and its stdout
  becomes the TUI status line; falls back to the built-in `model | perm | tokens`
  line when unconfigured.

## Build & run

> **Prerequisite:** lathe currently resolves `agentscope-go` from a **local
> sibling checkout**. Clone [agentscope-go](https://github.com/alanfokco/agentscope-go)
> at `../agentscope-go` (next to this repo) before building. The `replace`
> directive in `go.mod` will be dropped in favor of
> `go get github.com/alanfokco/agentscope-go@v2.0.3` once agentscope-go is
> published.

```bash
git clone https://github.com/AlanFokCo/lathe.git && cd lathe
git clone https://github.com/alanfokco/agentscope-go.git ../agentscope-go
go build -o lathe ./cmd/lathe
```

- Build all packages: `go build ./...`
- Test (hermetic — uses a fake model, **no API key needed**): `go test ./...`
- Vet: `go vet ./...`
- Run TUI: `./lathe` (or `go run ./cmd/lathe`)
- Print mode: `./lathe -p "your prompt"` (`--output stream-json` for NDJSON)

## Providers & credentials

Set an environment key, or pass `--provider` + `--api-key` (and `--base-url`):

| Provider | Env var | Default model |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` | `claude-sonnet-4-20250514` |
| `openai` | `OPENAI_API_KEY` | `gpt-4o-mini` |
| `dashscope` | `DASHSCOPE_API_KEY` | `qwen-plus` |
| `ollama` | (none — dummy key) | (pass `--model`) |

`--provider ollama` reuses the OpenAI client against `http://localhost:11434`
(Ollama needs no key, but the OpenAI client requires a non-empty one, so a dummy
is used); point `--base-url` elsewhere for lmstudio/vllm.

## Flags

`--prompt` / `-p`, `--provider`, `--model`, `--api-key`, `--base-url`,
`--permission` (default · accept_edits · explore · bypass · dont_ask),
`--max-iters`, `--sandbox` (none · docker · e2b), `--output` (text · stream-json),
`--resume <id>`, `--continue`.

Print-mode permission default is `accept_edits`; TUI defaults to `default` (asks).

## Configuration

lathe reads (project `<cwd>/.lathe/` overrides user `~/.lathe/`):

- **`settings.json`** — `hooks` (PreToolUse/PostToolUse/UserPromptSubmit/Stop)
  and `statusLine` (`{ "type": "command", "command": "...", "padding": N }`).
- **`.mcp.json`** — MCP servers (`stdio` / `http`; `sse` unsupported).
- **`skills/<name>/SKILL.md`** — instruction docs surfaced to the model.
- **`CLAUDE.md` / `AGENTS.md`** — project memory injected into the system
  prompt (walked cwd→repo-root, plus `~/.lathe/CLAUDE.md`).

Example `~/.lathe/settings.json` with a status line:

```json
{
  "statusLine": {
    "type": "command",
    "command": "jq -r '\"\\(.model.id) · in=\\(.context_window.total_input_tokens) out=\\(.context_window.total_output_tokens)\"'"
  }
}
```

## Architecture

- `cmd/lathe` (cobra) resolves `config.Config`, then forks to the **TUI**
  (`internal/tui`, bubbletea, alt-screen) or **print** (`internal/cli/print.go`
  via `internal/render`).
- Both call `agent.NewEngine(ctx, cfg)`; `Engine.Run(ctx, prompt)` returns an
  `<-chan event.Event`.
- **The turn loop** (`internal/agent/engine.go`) drives `model.ChatStream`
  directly: per iteration — (optional) auto-compact → `ChatStream` →
  `accumulate` → emit `TextDelta`/`Usage` → append assistant message → no tool
  calls = `end_turn`, else `dispatch` + append results as a **user-role**
  message (Anthropic requires `tool_result` in a user message) → repeat.
- **Event stream** (`internal/event`) is the central seam between engine and
  consumers — `render` (print) and `tui` are independent consumers; new
  turn-level signals go here.
- **Tools / permissions / hooks**: host builtins (`tool.NewEnhancedToolkit`) or
  workspace-backed (`--sandbox`); each call is checked against the permission
  engine; `settings.json` hooks fire at tool/turn boundaries.
- **Extension points**: skills (`internal/skills`), MCP (`internal/mcpconfig`),
  hooks (`internal/hooks` + `internal/settings`), Task subagent
  (`internal/agent/task_tool.go`), sandbox (`internal/workspace`).

## Project layout

```
cmd/lathe/         cobra entrypoint
internal/agent/    turn engine, factory, dispatch, compress, task tool, sysprompt, memory
internal/cli/      print mode
internal/config/   flag > env > defaults resolution
internal/event/    Event interface (engine → consumer seam)
internal/hooks/    shell-command hook runner
internal/mcpconfig/ .mcp.json parsing + MCP clients
internal/render/   text + stream-json renderers
internal/session/  JSONL transcripts, resume/continue
internal/settings/ settings.json (hooks + statusLine)
internal/skills/   SKILL.md discovery
internal/statusline/ configurable TUI status line (M5b)
internal/tui/      bubbletea TUI
internal/workspace/ docker/e2b sandbox + workspace-backed tools
```

## Conventions

- Code comments are tagged with milestone markers (`M2`, `M4c`, `M5a`, …) from
  local design docs.
- Config resolution order is **flag > env > defaults** (`internal/config`) —
  wire new options through `config.Flags` / `config.Config` + `Load` rather than
  reading env ad hoc.
- New turn-level signals go in `internal/event`; new tools go through the
  toolkit + permission engine (+ hook boundaries in `dispatch.go`).

See [`CLAUDE.md`](CLAUDE.md) for the full agent-facing guide.

## License

MIT — see [LICENSE](LICENSE).
