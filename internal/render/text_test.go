package render

import (
	"bytes"
	"strings"
	"testing"

	asevent "github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

func TestRenderText(t *testing.T) {
	evs := []asevent.Event{
		asevent.NewTextBlockDeltaEvent("", "", "Hel"),
		asevent.NewTextBlockDeltaEvent("", "", "lo"),
		asevent.NewToolCallStartEvent("", "t1", "Read"),
		asevent.NewToolResultStartEvent("", "t1", "Read"),
		asevent.NewToolResultTextDeltaEvent("", "t1", "file contents"),
		asevent.NewToolResultEndEvent("", "t1", message.ToolResultSuccess),
		asevent.NewModelCallStartEvent("", "gpt-4o"),
		asevent.NewModelCallEndEvent("", 1, 2),
		asevent.NewReplyEndEvent("", ""),
	}
	ch := make(chan asevent.Event, len(evs))
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
