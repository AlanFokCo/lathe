package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// atFileMaxBytes is the per-file inline cap for @path expansion (M7d.1). Chosen
// to keep an accidental @huge.log from blowing the context window; the model
// can still request larger files via the Read tool with pagination.
const atFileMaxBytes = 64 * 1024

// atFileRe matches @<path> tokens. Path characters are the ones common in
// unix paths (letters, digits, dot, slash, dash, underscore) so we do not
// greedily swallow adjacent punctuation like commas or parentheses.
var atFileRe = regexp.MustCompile(`@([A-Za-z0-9._/\-]+)`)

// expandAtFiles inlines the contents of each @path in prompt that resolves
// to a real, small, text file under cwd. Missing / binary / oversize / path-
// escape cases are left verbatim so a stray @mention does not silently
// change the prompt. Output wraps each inlined file with BEGIN/END markers
// so the model can distinguish injected content from the user's own text.
func expandAtFiles(prompt, cwd string) string {
	if cwd == "" || !strings.Contains(prompt, "@") {
		return prompt
	}
	rootAbs, err := filepath.Abs(cwd)
	if err != nil {
		return prompt
	}
	return atFileRe.ReplaceAllStringFunc(prompt, func(match string) string {
		rel := match[1:] // drop leading @
		if rel == "" {
			return match
		}
		full := filepath.Join(rootAbs, rel)
		abs, err := filepath.Abs(full)
		if err != nil {
			return match
		}
		// Reject path escapes: the resolved abs must live under rootAbs.
		if !strings.HasPrefix(abs+string(filepath.Separator), rootAbs+string(filepath.Separator)) && abs != rootAbs {
			return match
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() > atFileMaxBytes {
			return match
		}
		body, err := os.ReadFile(abs)
		if err != nil || !utf8.Valid(body) {
			return match
		}
		return "\n\n---BEGIN FILE @" + rel + "---\n" + string(body) + "\n---END FILE @" + rel + "---\n\n" + match
	})
}
