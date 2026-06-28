# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

lathe is a coding-agent CLI (in the mold of codex / claude-code) built in Go on top of `agentscope-go`. It runs an LLM in a tool-use loop against the local workspace, with an interactive TUI and a non-interactive print mode. Module: `github.com/alanfokco/lathe`, Go 1.26.

## Build, test, run

The module has a `replace` directive that resolves `agentscope-go` from a **sibling checkout at `../agentscope-go`** (see `go.mod`). That checkout must exist locally — `go build` / `go test` fail without it. (Before publishing, drop the replace and `go get github.com/alanfokco/agentscope-go@v2.0.3`.)

- Build: `go build ./...` — or produce the binary: `go build -o lathe ./cmd/lathe`
- Test (hermetic — uses a fake model, **no API key needed**): `go test ./...`
- Single test: `go test ./internal/agent -run TestPrintIntegration`
- Vet (only static check; no Makefile or linter config): `go vet ./...`
- Run TUI: `go run ./cmd/lathe` (or `./lathe`)
- Run print mode: `go run ./cmd/lathe -p "your prompt"` (`--output stream-json` for NDJSON)

Credentials/provider: set `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `DASHSCOPE_API_KEY`, or pass `--provider` + `--api-key` (+ `--base-url`). Providers: `anthropic`, `openai`, `dashscope`, `ollama` (ollama reuses the OpenAI client against `http://localhost:11434` with a dummy key). Other flags: `--model`, `--permission` (default|accept_edits|explore|bypass|dont_ask), `--max-iters`, `--sandbox` (none|docker|e2b), `--resume <id>`, `--continue`.

## Architecture

### Two entry modes, one engine
`cmd/lathe/main.go` (cobra) resolves `config.Config` then forks:
- `--prompt` set → `internal/cli/print.go` `RunPrint`: non-interactive, streams events through `internal/render` (text to stdout, or `stream-json` NDJSON).
- no prompt → `internal/tui` (bubbletea, alt-screen): interactive, prompts on permission "ask".

Both paths call `agent.NewEngine(ctx, cfg)` (`internal/agent/factory.go`), which assembles the model + toolkit + permission engine + all plugin discovery, then `eng.Run(ctx, prompt)` returns an `<-chan event.Event`.

### The turn loop (the heart)
`Engine` (`internal/agent/engine.go`) is **not** a wrapper around agentscope's `UnifiedAgent` — it drives `model.ChatStream` directly. Per iteration:
1. (optional) auto-compact if conversation tokens exceed threshold;
2. `ChatStream` → `accumulate` (collects text deltas, tool calls, usage);
3. emit `TextDelta`/`Usage` events; append an assistant message (text + tool calls);
4. no tool calls → `end_turn`; else `dispatch` the calls and append results as a **user-role** message; repeat until `end_turn` or `MaxIters`.

Cross-provider gotcha: tool results are appended in a **user-role** message because Anthropic requires `tool_result` blocks in a user message (an assistant-role one is invisible to the model and causes a "stdout wasn't returned" loop). OpenAI/Dashscope formatters override the role to `tool`, so they are unaffected.

### Event stream is the central seam
`internal/event` defines a closed `Event` interface (`TextDelta`, `ToolCallStart`, `ToolResult`, `Usage`, `ReplyEnd`, `ErrorEvent`, `Compacted`, `RequireApproval`) — all implementers live in this package. The engine emits on a buffered channel; `render` (print) and `tui` are independent consumers. New turn-level signals go here.

### Tools, permissions, hooks
- **Toolkit**: without `--sandbox`, lathe uses agentscope's host builtins (Bash/Read/Write/Edit/Glob/Grep). With `--sandbox`, `internal/workspace` rebuilds those tools to route through a `workspace.Workspace` (Docker container mounting cwd, or E2B cloud). Sandbox setup failure fails loudly — **no silent fallback to host execution**.
- **Permission** (`dispatch.go`): each tool call is checked against the permission engine. `ask` in print mode → deny; in TUI → emit `RequireApproval` and block on `approvalCh` (`allow`/`deny`/`always`).
- **Hooks** (`internal/hooks`): `settings.json` shell-command hooks fire at `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`. A hook receives JSON on stdin and may return JSON to block a tool (`{"decision":"block","reason":"..."}`) or inject context (`{"additionalContext":"..."}`). Failures are non-blocking.

### Extension discovery (all wired in `NewEngine`)
- **Skills** (`internal/skills`): instruction docs (`SKILL.md`) from `~/.lathe/skills` + project `.lathe/skills` (walked cwd→repo-root). Exposed via the read-only `SkillViewer` tool — skills are instructions the model reads, not callable tools.
- **MCP** (`internal/mcpconfig`): `.mcp.json` (project) + `~/.lathe/mcp.json` (user); `stdio` and `http` servers become ordinary toolkit tools. Project overrides user on name clash; `sse` is unsupported.
- **Settings/hooks** (`internal/settings`): `~/.lathe/settings.json` + `<cwd>/.lathe/settings.json`; a project event-list overrides a user event-list of the same key.
- **Task subagent** (`internal/agent/task_tool.go`): the `Task` tool spawns a nested `Engine` with a **builtins-only toolkit (no `Task` → no recursion)**, sharing the parent's model + permission engine (non-interactive). In sandbox mode the subagent shares the workspace.

### Session persistence
`internal/session` writes JSONL transcripts to `~/.lathe/projects/<enc-cwd>/<id>.jsonl` (claude-code-style project dirs; first line is metadata, the rest are messages). `--resume <id>` loads by ID (scans project dirs); `--continue` loads the newest in cwd.

### Auto-compact
`internal/agent/compress.go`: when conversation tokens exceed `0.8 × context_size`, older messages are summarized via structured output (`{task_overview, current_state, important_discoveries, next_steps, context_to_preserve}`), formatted into a template, and replace the old prefix; a `0.1 × ctx` tail is reserved (tool_call/result pairs kept together). `/compact` forces it. Emits a `Compacted` event.

### Project memory (meta)
`internal/agent/memory.go` loads `CLAUDE.md` / `AGENTS.md` from `~/.lathe/CLAUDE.md` plus a cwd→repo-root walk and injects them into the system prompt. **This file is read by lathe itself** when lathe runs against this repo — keep guidance here actionable for an agent, not just human-readable.

## Conventions
- Code comments are tagged with milestone markers (`M2`, `M4c`, `M5a`, …) from local design docs under `docs/superpowers/` (gitignored — local working plans/specs, not present on a fresh clone). When extending a subsystem, follow the existing `Mx:` tag style.
- Config resolution order is **flag > env > defaults** (`internal/config`). Wire new options through `config.Flags` / `config.Config` and `Load` rather than reading env ad hoc.
- New turn-level signals go in `internal/event`; new tools go through the toolkit + permission engine (+ hook boundaries in `dispatch.go`).
