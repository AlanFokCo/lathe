package tui

import (
	"regexp"
	"strings"
	"testing"
)

// stripANSI removes ANSI color/style escape sequences so assertions can check
// the rendered text content without per-word color codes splitting tokens.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestRenderMarkdownBold(t *testing.T) {
	got := stripANSI(RenderMarkdown("**hi**", 80))
	if strings.Contains(got, "**") {
		t.Fatalf("bold marker not consumed: %q", got)
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("text lost: %q", got)
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	got := stripANSI(RenderMarkdown("```go\nfunc f() {}\n```\n", 80))
	if strings.Contains(got, "```") {
		t.Fatalf("fence not consumed: %q", got)
	}
	if !strings.Contains(got, "func f()") {
		t.Fatalf("code block content lost: %q", got)
	}
}

func TestRenderMarkdownPlain(t *testing.T) {
	got := stripANSI(RenderMarkdown("Hello world", 80))
	if !strings.Contains(got, "Hello world") {
		t.Fatalf("plain text lost: %q", got)
	}
}
