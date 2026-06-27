package render

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/alanfokco/lathe/internal/event"
)

func TestRenderStreamJSON(t *testing.T) {
	evs := []event.Event{
		event.TextDelta{Delta: "hi"},
		event.ReplyEnd{Reason: "end_turn"},
	}
	ch := make(chan event.Event, len(evs))
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
		n++
	}
	if n != 2 {
		t.Fatalf("expected 2 lines, got %d", n)
	}
}
