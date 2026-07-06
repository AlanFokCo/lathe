package render

import (
	"context"
	"encoding/json"
	"io"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
)

// RenderStreamJSON writes one JSON object per agentscope event to out (NDJSON).
// Each line is `{"type":"<event_type>", ...event fields...}` so consumers can
// discriminate on type (agentscope events don't include type in their own JSON).
func RenderStreamJSON(ctx context.Context, ch <-chan asevent.Event, out io.Writer) {
	enc := json.NewEncoder(out)
	for ev := range ch {
		raw, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m == nil {
			m = map[string]any{}
		}
		m["type"] = string(ev.GetEventType())
		if err := enc.Encode(m); err != nil {
			return
		}
	}
}
