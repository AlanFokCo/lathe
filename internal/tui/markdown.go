package tui

import (
	"sync"

	"github.com/charmbracelet/glamour"
)

// rendererCache memoizes one glamour renderer per wrap width. glamour.NewTermRenderer
// parses style JSON + inits chroma, so rebuilding it per call (per token, per block)
// was real CPU/GC waste under the streaming rebuild. Keyed by width (style is fixed
// "dark" until the M6b theme system parameterizes it).
var (
	rendererMu    sync.Mutex
	rendererCache = map[int]*glamour.TermRenderer{}
)

func rendererFor(width int) (*glamour.TermRenderer, error) {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	if r, ok := rendererCache[width]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStandardStyle("dark"),
	)
	if err != nil {
		return nil, err
	}
	rendererCache[width] = r
	return r, nil
}

// RenderMarkdown renders md as ANSI-styled Markdown at width (M5c-2). On any
// error it returns the raw md unchanged (non-fatal — the caller falls back to
// raw text). M6a: uses a per-width cached renderer to avoid rebuilding glamour
// (style JSON parse + chroma init) on every call during streaming.
func RenderMarkdown(md string, width int) string {
	r, err := rendererFor(width)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
