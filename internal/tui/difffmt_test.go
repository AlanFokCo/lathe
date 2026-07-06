package tui

import (
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/tui/theme"
)

func TestRenderDiffPreservesTextAndGutter(t *testing.T) {
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n ctx\n-old\n+new\n"
	plain := stripANSI(renderDiff(diff, 80, theme.Dark()))
	for _, want := range []string{"ctx", "old", "new", "@@ -1,2 +1,2 @@"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in rendered diff:\n%s", want, plain)
		}
	}
	if !strings.Contains(plain, "2 +new") {
		t.Fatalf("added-line gutter (new line number) missing:\n%s", plain)
	}
}

func TestRenderDiffMalformedFallsBackToRaw(t *testing.T) {
	raw := "not a diff at all\njust some text"
	if got := renderDiff(raw, 80, theme.Dark()); got != raw {
		t.Fatalf("malformed diff should fall back to raw, got:\n%s", got)
	}
}

func TestParseHunkHeader(t *testing.T) {
	o, n := parseHunkHeader("@@ -12,3 +45,6 @@ func foo()")
	if o != 12 || n != 45 {
		t.Fatalf("parseHunkHeader = (%d,%d), want (12,45)", o, n)
	}
}
