package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/event"
)

func TestRenderText(t *testing.T) {
	evs := []event.Event{
		event.TextDelta{Delta: "Hel"},
		event.TextDelta{Delta: "lo"},
		event.ToolCallStart{ID: "t1", Name: "Read", Input: `{"path":"x"}`},
		event.ToolResult{ID: "t1", Name: "Read", Output: "file contents", State: "success"},
		event.Usage{InputTokens: 1, OutputTokens: 2, Model: "gpt-4o"},
		event.ReplyEnd{Reason: "end_turn"},
	}
	ch := make(chan event.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	RenderText(nil, ch, out, errOut)

	if out.String() != "Hello\n" {
		t.Fatalf("stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Read(") || !strings.Contains(errOut.String(), "file contents") || !strings.Contains(errOut.String(), "gpt-4o") {
		t.Fatalf("stderr: %q", errOut.String())
	}
}
