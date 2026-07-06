package tui

import (
	"fmt"
	"sync"

	"github.com/charmbracelet/glamour"
)

// rendererCache memoizes one glamour renderer per (width, style). glamour.NewTermRenderer
// parses style JSON + inits chroma, so rebuilding it per call (per token, per block)
// was real CPU/GC waste under the streaming rebuild.
var (
	rendererMu    sync.Mutex
	rendererCache = map[string]*glamour.TermRenderer{}
)

func rendererFor(width int, style string) (*glamour.TermRenderer, error) {
	key := fmt.Sprintf("%d|%s", width, style)
	rendererMu.Lock()
	defer rendererMu.Unlock()
	if r, ok := rendererCache[key]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStandardStyle(style),
	)
	if err != nil {
		return nil, err
	}
	rendererCache[key] = r
	return r, nil
}

// RenderMarkdown renders md as ANSI-styled Markdown at width using the current
// theme's glamour style (M6b). On any error it returns md unchanged (non-fatal).
func RenderMarkdown(md string, width int) string {
	r, err := rendererFor(width, curTheme.GlamourStyle)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
