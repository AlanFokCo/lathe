package render

import (
	"context"
	"encoding/json"
	"io"

	"github.com/alanfokco/lathe/internal/event"
)

// RenderStreamJSON writes one JSON object per event to out (NDJSON).
func RenderStreamJSON(ctx context.Context, ch <-chan event.Event, out io.Writer) {
	enc := json.NewEncoder(out)
	for ev := range ch {
		obj := map[string]any{"type": ev.Kind()}
		switch e := ev.(type) {
		case event.TextDelta:
			obj["delta"] = e.Delta
		case event.ToolCallStart:
			obj["id"], obj["name"], obj["input"] = e.ID, e.Name, e.Input
		case event.ToolResult:
			obj["id"], obj["name"], obj["output"], obj["state"], obj["diff"] = e.ID, e.Name, e.Output, e.State, e.Diff
		case event.Usage:
			obj["input_tokens"], obj["output_tokens"], obj["model"] = e.InputTokens, e.OutputTokens, e.Model
		case event.ReplyEnd:
			obj["reason"] = e.Reason
		case event.ErrorEvent:
			obj["error"] = e.Err.Error()
		}
		if err := enc.Encode(obj); err != nil {
			return
		}
	}
}
