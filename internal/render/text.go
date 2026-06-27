// Package render turns the lathe event stream into output. text.go is the
// human-readable print-mode view; streamjson.go is the NDJSON view. The M2
// TUI will be a third consumer of the same event stream.
package render

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alanfokco/lathe/internal/event"
)

// RenderText writes streamed text to out (stdout) and tool/usage annotations
// to errOut (stderr).
func RenderText(ctx context.Context, ch <-chan event.Event, out, errOut io.Writer) {
	for ev := range ch {
		switch e := ev.(type) {
		case event.TextDelta:
			fmt.Fprint(out, e.Delta)
		case event.ToolCallStart:
			fmt.Fprintf(errOut, "⏺ %s(%s)\n", e.Name, strings.TrimSpace(e.Input))
		case event.ToolResult:
			fmt.Fprintf(errOut, "  ↳ %s\n", strings.TrimSpace(e.Output))
		case event.Usage:
			fmt.Fprintf(errOut, "[tokens in=%d out=%d model=%s]\n", e.InputTokens, e.OutputTokens, e.Model)
		case event.ReplyEnd:
			fmt.Fprintln(out)
		case event.ErrorEvent:
			fmt.Fprintf(errOut, "error: %v\n", e.Err)
		}
	}
}
