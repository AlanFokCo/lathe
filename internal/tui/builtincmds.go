package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/pricing"
	tea "github.com/charmbracelet/bubbletea"
)

// costText reports cumulative token usage + a dollar-cost estimate when the
// provider+model matches a known rate (M6c-4, M9b). Cache tokens are shown
// when the provider reported any.
func (m *model) costText() string {
	s := fmt.Sprintf("tokens: in=%d out=%d", m.cumIn, m.cumOut)
	if m.cumCacheR > 0 || m.cumCacheW > 0 {
		s += fmt.Sprintf(" · cache: read=%d write=%d", m.cumCacheR, m.cumCacheW)
	}
	if est := m.dollarEstimate(); est != "" {
		s += "\n" + est
	}
	return s
}

// dollarEstimate returns "cost: $x.xxxx" when the model has a known rate,
// else "". Kept as a small helper so both /cost and the status line can call it.
func (m *model) dollarEstimate() string {
	if m.cfg == nil {
		return ""
	}
	r, ok := pricing.Lookup(m.cfg.Provider, m.cfg.Model)
	if !ok || r.Zero() {
		return ""
	}
	usd := r.Estimate(m.cumIn, m.cumOut, m.cumCacheR, m.cumCacheW)
	if usd < 0.0001 {
		return ""
	}
	return fmt.Sprintf("cost: $%.4f", usd)
}

// doctorText reports a quick diagnostic of the resolved config + environment
// (M6c-4). Marks required fields with ✓/✗; the rest are informational. M9a
// extends with agentscope build version, jail state, and MCP server count so
// users can audit the runtime at a glance.
func (m *model) doctorText() string {
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	sandbox := m.engine.SandboxMode()
	var b strings.Builder
	b.WriteString("doctor:\n")
	b.WriteString(fmt.Sprintf("  %s provider:   %s\n", mark(m.cfg.Provider != ""), m.cfg.Provider))
	b.WriteString(fmt.Sprintf("  %s model:      %s\n", mark(m.cfg.Model != ""), m.cfg.Model))
	b.WriteString(fmt.Sprintf("  %s api key:    %s\n", mark(m.cfg.APIKey != ""), redactKey(m.cfg.APIKey)))
	b.WriteString(fmt.Sprintf("  · permission: %s\n", m.cfg.Permission))
	b.WriteString(fmt.Sprintf("  · sandbox:    %s\n", sandbox))
	b.WriteString(fmt.Sprintf("  · jail:       %s\n", onOff(m.engine.Jailed())))
	b.WriteString(fmt.Sprintf("  · theme:      %s\n", curTheme.Name))
	b.WriteString(fmt.Sprintf("  · context:    %d tokens\n", m.ctxSize))
	if v := m.engine.AgentscopeVersion(); v != "" {
		b.WriteString(fmt.Sprintf("  · agentscope: %s\n", v))
	}
	if mcps := m.engine.MCPServers(); len(mcps) > 0 {
		b.WriteString(fmt.Sprintf("  · mcp:        %d server(s), %d tool(s)\n", len(mcps), mcpToolCount(mcps)))
	} else {
		b.WriteString("  · mcp:        none\n")
	}
	if m.cwd != "" {
		b.WriteString(fmt.Sprintf("  · cwd:        %s\n", m.cwd))
	}
	return strings.TrimRight(b.String(), "\n")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func mcpToolCount(servers []mcpconfig.ServerInfo) int {
	total := 0
	for _, s := range servers {
		total += s.ToolCount
	}
	return total
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

// skillsText renders the /skills output: one line per discovered skill with
// name + one-line description (M8c). Skills are read-only instructions the
// model can view via the SkillViewer tool.
func (m *model) skillsText() string {
	skills := m.engine.SkillsList()
	if len(skills) == 0 {
		return "/skills: no skills discovered (~/.lathe/skills or <cwd>/.lathe/skills)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("/skills: %d\n", len(skills)))
	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		b.WriteString(fmt.Sprintf("  %-24s %s\n", s.Name, desc))
	}
	return strings.TrimRight(b.String(), "\n")
}

// hooksText renders the /hooks output: settings.json hooks grouped by event
// (M8c). Each matcher shows its pattern and the commands that will run.
func (m *model) hooksText() string {
	hooks := m.engine.HooksList()
	if len(hooks) == 0 {
		return "/hooks: no hooks configured (~/.lathe/settings.json)"
	}
	events := make([]string, 0, len(hooks))
	for k := range hooks {
		events = append(events, k)
	}
	sort.Strings(events)
	var b strings.Builder
	b.WriteString("/hooks:\n")
	for _, ev := range events {
		b.WriteString("  " + ev + ":\n")
		for _, mt := range hooks[ev] {
			pat := mt.Matcher
			if pat == "" {
				pat = "*"
			}
			for _, c := range mt.Hooks {
				b.WriteString(fmt.Sprintf("    [%s] %s\n", pat, c.Command))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// sandboxText renders the /sandbox posture report (M7f): sandbox backend,
// workspace-root jail state, and cwd. A quick "am I safe?" for the user.
func (m *model) sandboxText() string {
	cwd, _, _, _ := m.engine.StatusInfo()
	jail := "off"
	if m.engine.Jailed() {
		jail = "on"
	}
	return fmt.Sprintf("/sandbox:\n  backend:      %s\n  workspace jail: %s\n  cwd:          %s",
		m.engine.SandboxMode(), jail, cwd)
}

// agentsText renders the /agents output: one line per subagent dispatched by
// the Task tool this session (M7e). Includes status + description + duration
// + output-byte size — enough to trace what the parent has offloaded without
// pulling the subagent transcript into the scrollback.
func (m *model) agentsText() string {
	agents := m.engine.Subagents()
	if len(agents) == 0 {
		return "/agents: no subagents dispatched this session"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("/agents: %d dispatch(es)\n", len(agents)))
	for _, a := range agents {
		short := a.ID
		if len(short) > 8 {
			short = short[:8]
		}
		desc := a.Description
		if desc == "" {
			desc = "(no description)"
		}
		line := fmt.Sprintf("  %s  %-10s  %s", short, a.Status, desc)
		if a.Duration > 0 {
			line += fmt.Sprintf("  (%s, %d bytes)", a.Duration.Truncate(time.Millisecond), a.OutputBytes)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

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

// handlePlan parses /plan [on|off|approve] and toggles read-only plan mode
// (M7g). M10b: "approve" exits plan mode + switches to accept_edits for
// seamless plan→execute transition. Empty args → report current state.
func (m *model) handlePlan(rest string) tea.Cmd {
	rest = strings.TrimSpace(strings.ToLower(rest))
	if rest == "" {
		state := "off"
		if m.engine.IsPlanMode() {
			state = "on"
		}
		m.sbAppendUser("/plan: " + state)
		return nil
	}
	switch rest {
	case "on":
		m.engine.EnterPlanMode()
		m.sbAppendUser("/plan: on (read-only until /plan off or /plan approve)")
	case "off":
		m.engine.ExitPlanMode()
		m.sbAppendUser("/plan: off")
	case "approve":
		if !m.engine.IsPlanMode() {
			m.sbAppendUser("/plan approve: not in plan mode")
			return nil
		}
		m.engine.ApprovePlan()
		m.sbAppendUser("/plan: approved — executing (mode: accept_edits)")
	default:
		m.sbAppendUser("/plan: invalid " + rest + " (want on|off|approve)")
	}
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

// auditText reads the last 20 entries from the audit JSONL file and renders
// them for /audit. Each entry is shown as a one-line summary.
func (m *model) auditText() string {
	path := m.engine.AuditPath()
	if path == "" {
		return "/audit: no audit log active"
	}
	f, err := os.Open(path)
	if err != nil {
		return "/audit: " + err.Error()
	}
	defer func() { _ = f.Close() }()

	// Read all lines, keep last 20.
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 {
		return "/audit: log is empty"
	}
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("/audit: last %d of %d entries (%s)\n", len(lines)-start, len(lines), path))
	for _, line := range lines[start:] {
		// Parse minimal fields for display.
		var entry struct {
			Action   string `json:"action"`
			ToolName string `json:"tool_name,omitempty"`
			Decision string `json:"decision,omitempty"`
			Error    string `json:"error,omitempty"`
		}
		if jerr := json.Unmarshal([]byte(line), &entry); jerr != nil {
			b.WriteString("  (parse error)\n")
			continue
		}
		summary := "  " + entry.Action
		if entry.ToolName != "" {
			summary += " " + entry.ToolName
		}
		if entry.Decision != "" {
			summary += " [" + entry.Decision + "]"
		}
		if entry.Error != "" {
			summary += " err=" + entry.Error
		}
		b.WriteString(summary + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
