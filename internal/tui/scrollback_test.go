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

	got := sb.render(80)
	for _, want := range []string{"do X", "Hello", "Read", "contents of x", "●"} {
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

func TestRenderShowsFormatted(t *testing.T) {
	var sb scrollback
	sb.appendAssistantText("**hi**")
	sb.formatPending(80)
	got := sb.render(80)
	if strings.Contains(got, "**") {
		t.Fatalf("render should show formatted (no **):\n%s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("render lost text:\n%s", got)
	}
}

func TestToolBlockStyled(t *testing.T) {
	var sb scrollback
	sb.appendTool("t1", "Bash", `{"command":"ls"}`)
	sb.finishTool("t1", "done", "success", "")
	got := sb.render(80)
	if !strings.Contains(got, "● Bash") || !strings.Contains(got, "✓") || !strings.Contains(got, "done") {
		t.Fatalf("tool block styling missing:\n%s", got)
	}
}

func TestUsageBlockRemoved(t *testing.T) {
	var sb scrollback
	sb.appendUsage(event.Usage{InputTokens: 1, OutputTokens: 2, Model: "gpt-4o"})
	if got := sb.render(80); strings.Contains(got, "gpt-4o") || strings.Contains(got, "[tokens") {
		t.Fatalf("usage block should not render:\n%s", got)
	}
}
