package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paletteItems returns the slash commands matching the current input when it is
// a bare "/prefix" (no space yet); nil otherwise. Drives the M6c-3 palette.
func paletteItems(input string) []command {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return nil
	}
	return matchCommands(strings.TrimPrefix(input, "/"))
}

// renderPalette renders a single-line command palette: matching command names
// (the selected one highlighted) + the selected command's description and a nav
// hint. Single-line so the pinned bottom area stays a stable height. M6c-3.
func renderPalette(items []command, cursor int) string {
	if len(items) == 0 {
		return ""
	}
	if cursor < 0 || cursor >= len(items) {
		cursor = 0
	}
	names := make([]string, len(items))
	for i, it := range items {
		if i == cursor {
			names[i] = selectedToolStyle.Render("/" + it.name)
		} else {
			names[i] = toolStyle.Render("/" + it.name)
		}
	}
	muted := lipgloss.NewStyle().Foreground(curTheme.Muted)
	return strings.Join(names, " ") + "  " + muted.Render(items[cursor].desc+" · ↑↓ Tab")
}
