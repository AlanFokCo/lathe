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

// argPaletteItems returns argument candidates when the input is "/cmd <prefix>"
// (M10g). Returns the command and filtered args, or nil args when there is no
// matching command with argsFn.
func argPaletteItems(input string, m *model) ([]string, string) {
	if !strings.HasPrefix(input, "/") || !strings.Contains(input, " ") {
		return nil, ""
	}
	sp := strings.IndexByte(input, ' ')
	cmdName := input[1:sp]
	prefix := strings.TrimSpace(input[sp+1:])
	cmd, ok := lookupCommand(cmdName)
	if !ok || cmd.argsFn == nil {
		return nil, ""
	}
	all := cmd.argsFn(m)
	if prefix == "" {
		return all, cmdName
	}
	lower := strings.ToLower(prefix)
	var out []string
	for _, a := range all {
		if strings.HasPrefix(strings.ToLower(a), lower) {
			out = append(out, a)
		}
	}
	return out, cmdName
}

// renderArgPalette renders a single-line argument completion palette (M10g).
func renderArgPalette(args []string, cursor int, cmdName string) string {
	if len(args) == 0 {
		return ""
	}
	if cursor < 0 || cursor >= len(args) {
		cursor = 0
	}
	names := make([]string, len(args))
	for i, a := range args {
		if i == cursor {
			names[i] = selectedToolStyle.Render(a)
		} else {
			names[i] = toolStyle.Render(a)
		}
	}
	muted := lipgloss.NewStyle().Foreground(curTheme.Muted)
	return strings.Join(names, " ") + "  " + muted.Render("/"+cmdName+" · ↑↓ Tab")
}

// renderFilePickerPanel renders the @file autocomplete panel (M9c). Single-
// line so the pinned bottom area stays a stable height; the selected file is
// highlighted and a Tab hint follows.
func renderFilePickerPanel(items []string, cursor int) string {
	if len(items) == 0 {
		return ""
	}
	if cursor < 0 || cursor >= len(items) {
		cursor = 0
	}
	names := make([]string, len(items))
	for i, p := range items {
		if i == cursor {
			names[i] = selectedToolStyle.Render("@" + p)
		} else {
			names[i] = toolStyle.Render("@" + p)
		}
	}
	muted := lipgloss.NewStyle().Foreground(curTheme.Muted)
	return strings.Join(names, " ") + "  " + muted.Render("↑↓ Tab (files)")
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
