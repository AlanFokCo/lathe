package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// command is one slash command: name, one-line help, and a handler taking the
// arg remainder and returning an optional tea.Cmd. commands() is the single
// source of truth for dispatch (maybeSlash), /help, and the completion palette
// (M6c). It is a function (not a package var) to avoid an init cycle with
// helpText, which iterates the registry.
type command struct {
	name   string
	desc   string
	run    func(m *model, rest string) tea.Cmd
	argsFn func(m *model) []string // M10g: optional argument completer
}

func commands() []command {
	return []command{
		{name: "help", desc: "list commands", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(helpText()); return nil }},
		{name: "clear", desc: "clear the scrollback", run: func(m *model, _ string) tea.Cmd { m.sb.clear(); m.rebuild(); return nil }},
		{name: "compact", desc: "summarize + compress the conversation", run: func(m *model, _ string) tea.Cmd {
			m.sbAppendUser("/compact: compressing...")
			engine := m.engine
			return func() tea.Msg {
				msg, err := engine.CompressNow(context.Background())
				if err != nil {
					return compactDoneMsg{err: err}
				}
				return compactDoneMsg{result: msg}
			}
		}},
		{name: "model", desc: "show or switch the model", run: func(m *model, rest string) tea.Cmd { return m.handleModel(rest) },
			argsFn: func(m *model) []string { return m.engine.ListModels() }},
		{name: "theme", desc: "show or switch the theme (lathe-dark|light)", run: func(m *model, rest string) tea.Cmd { return m.handleTheme(rest) },
			argsFn: func(_ *model) []string { return []string{"lathe-dark", "light"} }},
		{name: "config", desc: "show the resolved config", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(configString(m.cfg)); return nil }},
		{name: "cost", desc: "show token usage (input/output/cache)", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.costText()); return nil }},
		{name: "doctor", desc: "diagnose provider/model/config", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.doctorText()); return nil }},
		{name: "init", desc: "scaffold a CLAUDE.md in the cwd", run: func(m *model, _ string) tea.Cmd { return m.handleInit() }},
		{name: "mcp", desc: "list configured MCP servers", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.mcpText()); return nil }},
		{name: "resume", desc: "list historical sessions in the cwd", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.resumeText()); return nil }},
		{name: "tools", desc: "list tools exposed to the model", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.toolsText()); return nil }},
		{name: "thinking", desc: "toggle extended thinking (on|off|budget=N)", run: func(m *model, rest string) tea.Cmd { return m.handleThinking(rest) },
			argsFn: func(_ *model) []string { return []string{"on", "off"} }},
		{name: "effort", desc: "set reasoning effort (low|medium|high|off)", run: func(m *model, rest string) tea.Cmd { return m.handleEffort(rest) },
			argsFn: func(_ *model) []string { return []string{"low", "medium", "high", "off"} }},
		{name: "plan", desc: "plan mode (on|off|approve)", run: func(m *model, rest string) tea.Cmd { return m.handlePlan(rest) },
			argsFn: func(_ *model) []string { return []string{"on", "off", "approve"} }},
		{name: "agents", desc: "list subagent dispatches (Task tool)", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.agentsText()); return nil }},
		{name: "sandbox", desc: "report sandbox mode + workspace-root jail status", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.sandboxText()); return nil }},
		{name: "skills", desc: "list discovered skills", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.skillsText()); return nil }},
		{name: "hooks", desc: "list configured settings.json hooks", run: func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.hooksText()); return nil }},
		{name: "quit", desc: "exit lathe", run: func(m *model, _ string) tea.Cmd { return tea.Quit }},
	}
}

// lookupCommand finds a command by name.
func lookupCommand(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// matchCommands returns registry commands whose name has the given prefix
// (for the completion palette, M6c-3).
func matchCommands(prefix string) []command {
	var out []command
	for _, c := range commands() {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// helpText renders the command list for /help, generated from the registry.
func helpText() string {
	var b strings.Builder
	b.WriteString("commands:\n")
	for _, c := range commands() {
		b.WriteString(fmt.Sprintf("  /%-8s %s\n", c.name, c.desc))
	}
	return strings.TrimRight(b.String(), "\n")
}
