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
	name string
	desc string
	run  func(m *model, rest string) tea.Cmd
}

func commands() []command {
	return []command{
		{"help", "list commands", func(m *model, _ string) tea.Cmd { m.sbAppendUser(helpText()); return nil }},
		{"clear", "clear the scrollback", func(m *model, _ string) tea.Cmd { m.sb.clear(); m.rebuild(); return nil }},
		{"compact", "summarize + compress the conversation", func(m *model, _ string) tea.Cmd {
			msg, err := m.engine.CompressNow(context.Background())
			if err != nil {
				m.sbAppendUser("/compact: " + err.Error())
			} else {
				m.sbAppendUser("/compact: " + msg)
			}
			return nil
		}},
		{"model", "show or switch the model", func(m *model, rest string) tea.Cmd { return m.handleModel(rest) }},
		{"theme", "show or switch the theme (lathe-dark|light)", func(m *model, rest string) tea.Cmd { return m.handleTheme(rest) }},
		{"config", "show the resolved config", func(m *model, _ string) tea.Cmd { m.sbAppendUser(configString(m.cfg)); return nil }},
		{"cost", "show token usage (input/output/cache)", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.costText()); return nil }},
		{"doctor", "diagnose provider/model/config", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.doctorText()); return nil }},
		{"init", "scaffold a CLAUDE.md in the cwd", func(m *model, _ string) tea.Cmd { return m.handleInit() }},
		{"mcp", "list configured MCP servers", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.mcpText()); return nil }},
		{"resume", "list historical sessions in the cwd", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.resumeText()); return nil }},
		{"tools", "list tools exposed to the model", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.toolsText()); return nil }},
		{"thinking", "toggle extended thinking (on|off|budget=N)", func(m *model, rest string) tea.Cmd { return m.handleThinking(rest) }},
		{"effort", "set reasoning effort (low|medium|high|off)", func(m *model, rest string) tea.Cmd { return m.handleEffort(rest) }},
		{"plan", "plan mode (on|off|approve)", func(m *model, rest string) tea.Cmd { return m.handlePlan(rest) }},
		{"agents", "list subagent dispatches (Task tool)", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.agentsText()); return nil }},
		{"sandbox", "report sandbox mode + workspace-root jail status", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.sandboxText()); return nil }},
		{"skills", "list discovered skills", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.skillsText()); return nil }},
		{"hooks", "list configured settings.json hooks", func(m *model, _ string) tea.Cmd { m.sbAppendUser(m.hooksText()); return nil }},
		{"quit", "exit lathe", func(m *model, _ string) tea.Cmd { return tea.Quit }},
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
