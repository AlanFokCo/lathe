// Package theme defines lathe's TUI color palette and external-renderer style
// names (glamour + chroma). lipgloss auto-degrades hex colors for the terminal
// (truecolor → 256 → 16) and honors NO_COLOR, so a single hex palette works
// everywhere without manual capability detection.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme is a named color palette plus the glamour/chroma style names to match.
type Theme struct {
	Name    string
	User    lipgloss.Color // user prompt marker
	Tool    lipgloss.Color // tool call bullets
	Muted   lipgloss.Color // secondary text / context lines
	Success lipgloss.Color
	Error   lipgloss.Color
	Warn    lipgloss.Color
	Accent  lipgloss.Color // headers / selection
	DiffAdd lipgloss.Color
	DiffDel lipgloss.Color
	Hunk    lipgloss.Color

	GlamourStyle string // glamour standard style: "dark" | "light"
	ChromaStyle  string // chroma style name for syntax highlighting
}

// Dark is the default theme (Tokyo Night-ish, truecolor).
func Dark() Theme {
	return Theme{
		Name:    "lathe-dark",
		User:    "#7aa2f7",
		Tool:    "#7dcfff",
		Muted:   "#565f89",
		Success: "#9ece6a",
		Error:   "#f7768e",
		Warn:    "#e0af68",
		Accent:  "#bb9af7",
		DiffAdd: "#9ece6a",
		DiffDel: "#f7768e",
		Hunk:    "#7dcfff",

		GlamourStyle: "dark",
		ChromaStyle:  "tokyonight-storm",
	}
}

// Light is a light-background theme.
func Light() Theme {
	return Theme{
		Name:    "light",
		User:    "#2e7de9",
		Tool:    "#007197",
		Muted:   "#8990b3",
		Success: "#587539",
		Error:   "#f52a65",
		Warn:    "#8c6c3e",
		Accent:  "#9854f1",
		DiffAdd: "#587539",
		DiffDel: "#f52a65",
		Hunk:    "#007197",

		GlamourStyle: "light",
		ChromaStyle:  "github",
	}
}

// Get returns the named theme, defaulting to Dark for empty/unknown names.
func Get(name string) Theme {
	switch name {
	case "light":
		return Light()
	default:
		return Dark()
	}
}
