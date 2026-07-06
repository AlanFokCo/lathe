package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alanfokco/lathe/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

// renderDiff renders a unified diff with a line-number gutter and themed
// +/-/context colors. Falls back to the raw text when the input has no hunks
// (so a non-diff string still renders). M6b.
func renderDiff(diff string, width int, th theme.Theme) string {
	_ = width // reserved for future ANSI-aware wrapping
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	addSt := lipgloss.NewStyle().Foreground(th.DiffAdd)
	delSt := lipgloss.NewStyle().Foreground(th.DiffDel)
	ctxSt := lipgloss.NewStyle().Foreground(th.Muted)
	hunkSt := lipgloss.NewStyle().Foreground(th.Hunk)
	hdrSt := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	gutSt := lipgloss.NewStyle().Foreground(th.Muted)
	gut := func(o, n int) string {
		s := "    "
		switch {
		case n >= 0:
			s = fmt.Sprintf("%4d", n)
		case o >= 0:
			s = fmt.Sprintf("%4d", o)
		}
		return gutSt.Render(s) + " "
	}
	var b strings.Builder
	oldLn, newLn := 0, 0
	sawHunk := false
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "@@"):
			sawHunk = true
			oldLn, newLn = parseHunkHeader(ln)
			b.WriteString(hunkSt.Render(ln) + "\n")
		case strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"),
			strings.HasPrefix(ln, "diff "), strings.HasPrefix(ln, "index "):
			b.WriteString(hdrSt.Render(ln) + "\n")
		case strings.HasPrefix(ln, "\\"): // "\ No newline at end of file"
			b.WriteString(ctxSt.Render(ln) + "\n")
		case strings.HasPrefix(ln, "+"):
			b.WriteString(gut(-1, newLn) + addSt.Render(ln) + "\n")
			newLn++
		case strings.HasPrefix(ln, "-"):
			b.WriteString(gut(oldLn, -1) + delSt.Render(ln) + "\n")
			oldLn++
		default: // context line (leading space or blank)
			b.WriteString(gut(oldLn, newLn) + ctxSt.Render(ln) + "\n")
			oldLn++
			newLn++
		}
	}
	if !sawHunk {
		return diff
	}
	return b.String()
}

// parseHunkHeader extracts the old/new starting line numbers from a
// "@@ -a,b +c,d @@" hunk header. Defaults to 1 on parse failure.
func parseHunkHeader(ln string) (oldStart, newStart int) {
	oldStart, newStart = 1, 1
	for _, f := range strings.Fields(ln) {
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case '-':
			oldStart = atoiPrefix(f[1:])
		case '+':
			newStart = atoiPrefix(f[1:])
		}
	}
	return
}

// atoiPrefix parses the leading integer of "N" or "N,M".
func atoiPrefix(s string) int {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}
