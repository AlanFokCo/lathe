package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// costText reports cumulative token usage for the session (M6c-4). Cache tokens
// are shown when the provider reported any.
func (m *model) costText() string {
	s := fmt.Sprintf("tokens: in=%d out=%d", m.cumIn, m.cumOut)
	if m.cumCacheR > 0 || m.cumCacheW > 0 {
		s += fmt.Sprintf(" · cache: read=%d write=%d", m.cumCacheR, m.cumCacheW)
	}
	return s
}

// doctorText reports a quick diagnostic of the resolved config + environment
// (M6c-4). Marks required fields with ✓/✗; the rest are informational.
func (m *model) doctorText() string {
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	sandbox := m.cfg.Sandbox
	if sandbox == "" {
		sandbox = "host"
	}
	var b strings.Builder
	b.WriteString("doctor:\n")
	b.WriteString(fmt.Sprintf("  %s provider:   %s\n", mark(m.cfg.Provider != ""), m.cfg.Provider))
	b.WriteString(fmt.Sprintf("  %s model:      %s\n", mark(m.cfg.Model != ""), m.cfg.Model))
	b.WriteString(fmt.Sprintf("  %s api key:    %s\n", mark(m.cfg.APIKey != ""), redactKey(m.cfg.APIKey)))
	b.WriteString(fmt.Sprintf("  · permission: %s\n", m.cfg.Permission))
	b.WriteString(fmt.Sprintf("  · sandbox:    %s\n", sandbox))
	b.WriteString(fmt.Sprintf("  · theme:      %s\n", curTheme.Name))
	b.WriteString(fmt.Sprintf("  · context:    %d tokens\n", m.ctxSize))
	if m.cwd != "" {
		b.WriteString(fmt.Sprintf("  · cwd:        %s\n", m.cwd))
	}
	return strings.TrimRight(b.String(), "\n")
}

// claudeMDTemplate is the starter CLAUDE.md written by /init.
const claudeMDTemplate = `# CLAUDE.md

Guidance for coding agents (lathe / claude-code) working in this repository.

## What this is
<one-paragraph description of the project>

## Build, test, run
- Build:
- Test:
- Run:

## Conventions
-
`

// toolsText renders the /tools output: the full alphabetized list of tools
// the model can call this turn (M6f). Includes builtins (Bash/Read/Edit/
// MultiEdit/ApplyPatch/…), MCP-discovered tools, skills viewer, and Task.
func (m *model) toolsText() string {
	names := m.engine.ToolNames()
	if len(names) == 0 {
		return "/tools: no tools registered"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("/tools: %d tool(s)\n", len(names)))
	for _, n := range names {
		b.WriteString("  " + n + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// mcpText renders the /mcp output: one line per configured MCP server with its
// tool count, or a "no MCP servers configured" hint (M6c-5). The snapshot is
// taken by NewEngine so this is just a read.
func (m *model) mcpText() string {
	servers := m.engine.MCPServers()
	if len(servers) == 0 {
		return "/mcp: no MCP servers configured"
	}
	var b strings.Builder
	b.WriteString("/mcp:\n")
	for _, s := range servers {
		b.WriteString(fmt.Sprintf("  %s (%d tools)\n", s.Name, s.ToolCount))
	}
	return strings.TrimRight(b.String(), "\n")
}

// resumeText renders the /resume output: one line per session (id / model /
// mtime / first prompt), plus the `lathe --resume <id>` command hint. In-
// process reload is deferred (M6c-5); the user restarts the CLI to resume.
func (m *model) resumeText() string {
	sessions := m.engine.ListSessions()
	if len(sessions) == 0 {
		return "/resume: no sessions found in this directory"
	}
	var b strings.Builder
	b.WriteString("/resume:\n")
	for _, s := range sessions {
		short := s.ID
		if len(short) > 8 {
			short = short[:8]
		}
		prompt := strings.ReplaceAll(s.FirstPrompt, "\n", " ")
		const maxPrompt = 60
		if len(prompt) > maxPrompt {
			prompt = prompt[:maxPrompt] + "…"
		}
		mtime := s.ModTime.Format(time.RFC3339)
		b.WriteString(fmt.Sprintf("  %s  %-18s  %s  %q\n", short, s.Model, mtime, prompt))
	}
	b.WriteString("\nrun: lathe --resume <id>")
	return strings.TrimRight(b.String(), "\n")
}

// handleThinking parses `/thinking [on|off] [budget=N]` and calls into the
// engine (M7a). Empty args → report current state. Tokens are case-insensitive
// and order-independent; `/thinking on budget=8000` and `/thinking budget=8000
// on` are equivalent.
func (m *model) handleThinking(rest string) tea.Cmd {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		en, bud := m.engine.Thinking()
		state := "off"
		if en {
			state = "on"
		}
		msg := fmt.Sprintf("/thinking: %s", state)
		if bud > 0 {
			msg += fmt.Sprintf(" (budget=%d)", bud)
		}
		m.sbAppendUser(msg)
		return nil
	}
	en, bud := m.engine.Thinking()
	toggled := false
	for _, tok := range strings.Fields(rest) {
		switch {
		case strings.EqualFold(tok, "on"):
			en = true
			toggled = true
		case strings.EqualFold(tok, "off"):
			en = false
			toggled = true
		case strings.HasPrefix(strings.ToLower(tok), "budget="):
			if n, err := strconv.Atoi(tok[len("budget="):]); err == nil && n > 0 {
				bud = n
			}
		}
	}
	if !toggled && bud > 0 {
		en = true // /thinking budget=N implicitly enables
	}
	m.engine.SetThinking(en, bud)
	state := "off"
	if en {
		state = "on"
	}
	msg := fmt.Sprintf("/thinking: %s", state)
	if bud > 0 {
		msg += fmt.Sprintf(" (budget=%d)", bud)
	}
	m.sbAppendUser(msg)
	return nil
}

// handleEffort parses /effort [low|medium|high|off] and calls the engine
// (M7b). Empty args → report current level. "off"/"none" clears the level.
// Unknown values print an error and leave the level unchanged.
func (m *model) handleEffort(rest string) tea.Cmd {
	rest = strings.TrimSpace(strings.ToLower(rest))
	if rest == "" {
		lvl := m.engine.Effort()
		if lvl == "" {
			lvl = "off"
		}
		m.sbAppendUser("/effort: " + lvl)
		return nil
	}
	switch rest {
	case "off", "none":
		m.engine.SetEffort("")
		m.sbAppendUser("/effort: off")
	case "low", "medium", "high":
		m.engine.SetEffort(rest)
		m.sbAppendUser("/effort: " + rest)
	default:
		m.sbAppendUser("/effort: invalid level " + rest + " (want low|medium|high|off)")
	}
	return nil
}

// handleInit scaffolds a CLAUDE.md in the working directory if absent (M6c-4).
func (m *model) handleInit() tea.Cmd {
	path := "CLAUDE.md"
	if m.cwd != "" {
		path = filepath.Join(m.cwd, "CLAUDE.md")
	}
	if _, err := os.Stat(path); err == nil {
		m.sbAppendUser("/init: CLAUDE.md already exists at " + path)
		return nil
	}
	if err := os.WriteFile(path, []byte(claudeMDTemplate), 0o644); err != nil {
		m.sbAppendUser("/init: " + err.Error())
		return nil
	}
	m.sbAppendUser("/init: wrote " + path)
	return nil
}
