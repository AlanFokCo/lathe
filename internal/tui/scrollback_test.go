package tui

import (
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/event"
)

func TestScrollbackRendersTextAndTool(t *testing.T) {
	var sb scrollback
	sb.appendUser("do X")
	sb.appendAssistantText("Hel")
	sb.appendAssistantText("lo")
	sb.appendTool("t1", "Read", `{"path":"x"}`)
	sb.finishTool("t1", "contents of x", "success", "")
	sb.appendUsage(event.Usage{InputTokens: 1, OutputTokens: 2, Model: "gpt-4o"})

	got := sb.render(80)
	for _, want := range []string{"do X", "Hello", "Read", "contents of x", "gpt-4o"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render missing %q:\n%s", want, got)
		}
	}
}

func TestScrollbackClear(t *testing.T) {
	var sb scrollback
	sb.appendUser("hi")
	sb.clear()
	if got := sb.render(80); got != "" {
		t.Fatalf("expected empty after clear, got %q", got)
	}
}

func TestFormatPendingRendersDirtyBlock(t *testing.T) {
	var s scrollback
	s.appendAssistantText("**hi**")
	if !s.blocks[0].dirty || s.blocks[0].formatted != "" {
		t.Fatalf("expected dirty + empty formatted: %+v", s.blocks[0])
	}
	s.formatPending(80)
	if s.blocks[0].dirty || s.blocks[0].formatted == "" {
		t.Fatalf("expected clean + formatted set: %+v", s.blocks[0])
	}
	if strings.Contains(s.blocks[0].formatted, "**") {
		t.Fatalf("formatted still has **: %q", s.blocks[0].formatted)
	}
}
