package tui

import (
	"strings"
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
