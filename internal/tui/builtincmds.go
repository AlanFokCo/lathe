package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
