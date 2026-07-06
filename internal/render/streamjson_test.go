package render

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
)

func TestRenderStreamJSON(t *testing.T) {
	evs := []asevent.Event{
		asevent.NewTextBlockDeltaEvent("", "", "hi"),
		asevent.NewReplyEndEvent("", ""),
	}
	ch := make(chan asevent.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)

	out := &bytes.Buffer{}
	RenderStreamJSON(nil, ch, out)

	sc := bufio.NewScanner(out)
	var n int
	for sc.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(sc.Bytes(), &obj); err != nil {
			t.Fatalf("invalid json line %q: %v", sc.Text(), err)
		}
		if obj["type"] == nil {
			t.Fatalf("line missing type field: %q", sc.Text())
		}
		n++
	}
	if n != 2 {
		t.Fatalf("expected 2 lines, got %d", n)
	}
}
