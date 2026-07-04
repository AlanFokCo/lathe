// Package render turns the agentscope event stream into output. text.go is the
// human-readable print-mode view; streamjson.go is the NDJSON view. M6a Commit
// B: consumes agentscope block-lifecycle events directly (no translator, no
// internal/event).
package render

import (
	"context"
	"fmt"
	"io"
	"strings"

	asevent "github.com/alanfokco/agentscope-go/pkg/agentscope/event"
)

// RenderText writes streamed text to out (stdout) and tool/usage annotations
// to errOut (stderr). Tool-result text deltas are accumulated per tool-call ID
// and flushed as one "↳ output" line on ToolResultEnd.
func RenderText(ctx context.Context, ch <-chan asevent.Event, out, errOut io.Writer) {
	pending := map[string]*strings.Builder{} // toolCallID → accumulated output
	var modelName string
	for ev := range ch {
		switch e := ev.(type) {
		case asevent.TextBlockDeltaEvent:
			fmt.Fprint(out, e.Delta)
		case asevent.ModelCallStartEvent:
			modelName = e.ModelName
		case asevent.ModelCallEndEvent:
			fmt.Fprintf(errOut, "[tokens in=%d out=%d model=%s]\n", e.InputTokens, e.OutputTokens, modelName)
		case asevent.ToolCallStartEvent:
			// agentscope's ToolCallStartEvent carries only ID+Name (no Input).
			fmt.Fprintf(errOut, "⏺ %s()\n", e.ToolCallName)
		case asevent.ToolResultStartEvent:
			if _, ok := pending[e.ToolCallID]; !ok {
				pending[e.ToolCallID] = &strings.Builder{}
			}
		case asevent.ToolResultTextDeltaEvent:
			if b, ok := pending[e.ToolCallID]; ok {
				b.WriteString(e.Delta)
			}
		case asevent.ToolResultEndEvent:
			var outText string
			if b, ok := pending[e.ToolCallID]; ok {
				outText = b.String()
				delete(pending, e.ToolCallID)
			}
			fmt.Fprintf(errOut, "  ↳ %s\n", strings.TrimSpace(outText))
		case asevent.ReplyEndEvent:
			fmt.Fprintln(out)
		}
	}
}
