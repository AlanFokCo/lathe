package tui

import (
	"fmt"
	"strings"

	"github.com/muesli/reflow/wordwrap"
)

// insideOpenFence reports whether text ends inside an unclosed ``` or ~~~
// fenced code block (M5d). Used by commitLenFor to decide whether to commit
// the whole text (in-fence streaming, render the growing code block in place)
// or only up to the last newline.
func insideOpenFence(text string) bool {
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
	}
	return inFence
}

// commitLenFor returns the byte offset into text up to which is safe to commit
// to glamour (M5d). Inside an open fence: len(text) (render the growing code
// block in place, partial line included). Otherwise: after the last '\n'
// (complete lines only; the partial last line stays as raw pending). No
// newline yet: 0 (all pending — visually identical to glamour for plain prose).
//
// The returned offset is a safe UTF-8 cut point because it is either len(text)
// or a position immediately after a '\n' (ASCII, single-byte).
func commitLenFor(text string) int {
	if insideOpenFence(text) {
		return len(text)
	}
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		return i + 1
	}
	return 0
}

// summarize produces a one-line summary of a tool call's output for the
// collapsed tool block (M5d). Pure function; cached on the block at
// finishTool time.
func summarize(name, output, state, diff string) string {
	if diff != "" {
		return "edited " + diffstat(diff)
	}
	out := strings.TrimSpace(output)
	switch name {
	case "Read":
		if out == "" {
			return "→ (no output)"
		}
		if strings.Contains(output, "\n") {
			return fmt.Sprintf("→ %d lines", strings.Count(output, "\n")+1)
		}
		return fmt.Sprintf("→ %d chars", len(out))
	case "Bash":
		if out == "" {
			if state == "error" {
				return "→ exit"
			}
			return "→ (no output)"
		}
		return truncate(strings.Split(out, "\n")[0], 60)
	default:
		if out == "" {
			return "→ (no output)"
		}
		return "→ " + truncate(strings.Split(out, "\n")[0], 60)
	}
}

// diffstat returns a compact "+N -M" stat from a unified diff (M5d).
func diffstat(diff string) string {
	add, del := 0, 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "\\"):
			continue
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	return fmt.Sprintf("+%d -%d", add, del)
}

// truncate rune-safely truncates s to n runes, appending "…" if cut (M5d).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// wrapRaw word-wraps s at width for the raw pending tail of a streaming
// assistant block (M5d), so long lines don't blow past the viewport.
func wrapRaw(s string, width int) string {
	if width <= 0 {
		width = 80
	}
	return wordwrap.String(s, width)
}
