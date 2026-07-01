package tui

import "github.com/charmbracelet/glamour"

// RenderMarkdown renders md as ANSI-styled Markdown at width (M5c-2). On any
// error it returns the raw md unchanged (non-fatal — the caller falls back to
// raw text). Used by scrollback.formatPending for live, throttled rendering.
func RenderMarkdown(md string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStandardStyle("dark"),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
