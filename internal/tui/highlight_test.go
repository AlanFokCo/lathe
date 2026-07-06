package tui

import (
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/tui/theme"
)

func TestHighlightCodePreservesText(t *testing.T) {
	out := stripANSI(highlightCode("package main\nfunc main() {}\n", "x.go", theme.Dark()))
	if !strings.Contains(out, "package main") || !strings.Contains(out, "func main()") {
		t.Fatalf("highlight lost code text:\n%s", out)
	}
}

func TestHighlightCodeUnknownExtStillReturnsText(t *testing.T) {
	src := "just some plain text without a language"
	out := stripANSI(highlightCode(src, "notes.unknownext", theme.Dark()))
	if !strings.Contains(out, "plain text") {
		t.Fatalf("unknown ext should still return the text:\n%s", out)
	}
}

func TestFilenameFromToolInput(t *testing.T) {
	if got := filenameFromToolInput(`{"path":"internal/x.go"}`); got != "internal/x.go" {
		t.Fatalf("path: %q", got)
	}
	if got := filenameFromToolInput(`{"file_path":"a/b.py"}`); got != "a/b.py" {
		t.Fatalf("file_path: %q", got)
	}
	if got := filenameFromToolInput(`not json`); got != "" {
		t.Fatalf("bad json should be empty: %q", got)
	}
}

func TestToolHeaderArg(t *testing.T) {
	if got := toolHeaderArg("Read", `{"path":"x.go"}`); got != "x.go" {
		t.Fatalf("Read header: %q", got)
	}
	if got := toolHeaderArg("Bash", `{"command":"ls -la"}`); got != "ls -la" {
		t.Fatalf("Bash header: %q", got)
	}
	if got := toolHeaderArg("Read", ""); got != "" {
		t.Fatalf("empty input header: %q", got)
	}
}
