package tui

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/alanfokco/lathe/internal/tui/theme"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/quick"
)

// highlightCode syntax-highlights src for the terminal, choosing a lexer from
// filename (chroma falls back to content analysis / plaintext for unknowns).
// Returns src unchanged when NO_COLOR is set or on any error. M6b.
func highlightCode(src, filename string, th theme.Theme) string {
	if os.Getenv("NO_COLOR") != "" {
		return src
	}
	lexer := ""
	if l := lexers.Match(filename); l != nil {
		lexer = l.Config().Name
	}
	var buf strings.Builder
	if err := quick.Highlight(&buf, src, lexer, "terminal256", th.ChromaStyle); err != nil {
		return src
	}
	return buf.String()
}

// filenameFromToolInput best-effort extracts a file path from a tool call's
// JSON input (Read/Edit/Write), for lexer selection. Empty if none found.
func filenameFromToolInput(in string) string {
	var m map[string]any
	if json.Unmarshal([]byte(in), &m) != nil {
		return ""
	}
	for _, k := range []string{"path", "file_path", "filename", "file"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// toolHeaderArg renders a concise argument for the tool block header from the
// tool's JSON input — the full JSON (e.g. Edit's old/new strings) would be
// noise. Falls back to a truncated raw input for unknown shapes. M6b.
func toolHeaderArg(name, input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(input), &m) != nil {
		return truncate(strings.TrimSpace(input), 60)
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch name {
	case "Read", "Edit", "Write", "NotebookEdit":
		return str("path", "file_path", "filename", "file")
	case "Bash":
		return truncate(str("command"), 60)
	case "Grep", "Glob":
		return str("pattern", "query")
	default:
		return truncate(strings.TrimSpace(input), 60)
	}
}
