package tui

import (
	"strings"

	"github.com/alanfokco/lathe/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

type blockKind int

const (
	kindUser blockKind = iota
	kindAssistant
	kindTool
	kindError
)

// block is one scrollback entry. M5d upgrades the assistant streaming fields
// (committed/commitLen/fmtWidth/done) and adds tool summary/expand fields.
// M6a Commit B: tool output is accumulated from ToolResultTextDeltaEvent by
// tool-call ID (appendToolResultDelta), then finishTool marks done.
type block struct {
	kind blockKind
	text string // assistant: full raw streamed text
	// assistant streaming state (M5d):
	committed string // glamour(text[:commitLen]) cache
	commitLen int    // committed byte offset
	fmtWidth  int    // width committed was rendered at (resize detect)
	done      bool   // assistant: ReplyEnd reached; tool: finishTool called
	// tool fields:
	toolID    string
	toolName  string
	toolIn    string
	toolOut   string
	toolState string
	diff      string
	summary   string // one-line summary, cached at finishTool (M5d)
	expanded  bool   // M5d: inline full output when true
	startLine int    // M5d: line offset in built content (selection scroll)
}

type scrollback struct {
	blocks        []block
	lastAssistant int
}

func (s *scrollback) appendUser(prompt string) {
	s.blocks = append(s.blocks, block{kind: kindUser, text: prompt})
	s.lastAssistant = -1
}

// appendAssistantText appends a streaming delta. M5d iron rule 1: it MUST NOT
// clear committed or reset commitLen — the boundary-commit in build() decides
// when to re-glamour. Clearing here was the M5c-2 flicker root cause.
func (s *scrollback) appendAssistantText(delta string) {
	if s.lastAssistant >= 0 && s.lastAssistant < len(s.blocks) && s.blocks[s.lastAssistant].kind == kindAssistant {
		s.blocks[s.lastAssistant].text += delta
		return
	}
	s.blocks = append(s.blocks, block{kind: kindAssistant, text: delta})
	s.lastAssistant = len(s.blocks) - 1
}

// finishAssistant marks the last assistant block done (on ReplyEnd). M5d.
func (s *scrollback) finishAssistant() {
	if s.lastAssistant >= 0 && s.lastAssistant < len(s.blocks) && s.blocks[s.lastAssistant].kind == kindAssistant {
		s.blocks[s.lastAssistant].done = true
	}
	s.lastAssistant = -1
}

func (s *scrollback) appendTool(id, name, input string) {
	s.blocks = append(s.blocks, block{kind: kindTool, toolID: id, toolName: name, toolIn: input})
	s.lastAssistant = -1
}

// appendToolResultDelta accumulates a tool-result text delta into the matching
// (by tool-call ID) unfinished tool block (M6a Commit B).
func (s *scrollback) appendToolResultDelta(id, delta string) {
	for i := len(s.blocks) - 1; i >= 0; i-- {
		if s.blocks[i].kind == kindTool && s.blocks[i].toolID == id && !s.blocks[i].done {
			s.blocks[i].toolOut += delta
			return
		}
	}
}

// finishTool marks the matching (by tool-call ID) unfinished tool block done
// and caches its one-line summary (M5d, M6a Commit B).
func (s *scrollback) finishTool(id, state, diff string) {
	for i := len(s.blocks) - 1; i >= 0; i-- {
		if s.blocks[i].kind == kindTool && s.blocks[i].toolID == id && !s.blocks[i].done {
			b := &s.blocks[i]
			b.toolState = state
			b.diff = diff
			b.summary = summarize(b.toolName, b.toolOut, state, diff)
			b.done = true
			return
		}
	}
}

func (s *scrollback) appendError(err error) {
	s.blocks = append(s.blocks, block{kind: kindError, text: err.Error()})
	s.lastAssistant = -1
}

func (s *scrollback) clear() { s.blocks = nil; s.lastAssistant = 0 }

// invalidateRenders forces assistant blocks to re-run glamour on the next build
// (e.g. after a theme switch changes the glamour style). M6c.
func (s *scrollback) invalidateRenders() {
	for i := range s.blocks {
		s.blocks[i].fmtWidth = -1
	}
}

// M6b: styles are driven by the active theme. curTheme + the style vars are
// package-level and mutated only from the bubbletea Update goroutine (startup
// via init, and /theme switch), so no synchronization is needed.
var (
	curTheme = theme.Dark()

	userStyle         lipgloss.Style
	toolStyle         lipgloss.Style
	errorStyle        lipgloss.Style
	successStyle      lipgloss.Style
	warnStyle         lipgloss.Style
	promptStyle       lipgloss.Style
	selectedToolStyle lipgloss.Style
)

func init() { applyTheme(curTheme) }

// applyTheme sets the package-level styles + curTheme from th.
func applyTheme(th theme.Theme) {
	curTheme = th
	userStyle = lipgloss.NewStyle().Foreground(th.User)
	toolStyle = lipgloss.NewStyle().Foreground(th.Tool)
	errorStyle = lipgloss.NewStyle().Foreground(th.Error)
	successStyle = lipgloss.NewStyle().Foreground(th.Success)
	warnStyle = lipgloss.NewStyle().Foreground(th.Warn)
	promptStyle = lipgloss.NewStyle().Foreground(th.User)
	selectedToolStyle = lipgloss.NewStyle().Bold(true).Foreground(th.Accent)
}

// build produces the full scrollback content string at width (M5d). Replaces
// the old render+formatPending. glamour runs ONLY here, and only when a
// boundary advances or width changed. Called by the throttled formatTick and
// structural events; NEVER from View() (iron rule 2).
func (s *scrollback) build(width, selectedTool int) string {
	var b strings.Builder
	line := 0
	for i := range s.blocks {
		bl := &s.blocks[i]
		bl.startLine = line
		var body string
		switch bl.kind {
		case kindUser:
			body = userStyle.Render("> ") + bl.text + "\n"
		case kindAssistant:
			body = s.buildAssistant(bl, width)
		case kindTool:
			body = s.buildTool(bl, width, selectedTool == i) // M5d: highlight selected tool block
		case kindError:
			body = errorStyle.Render("\nerror: " + bl.text + "\n")
		}
		b.WriteString(body)
		line += strings.Count(body, "\n")
	}
	return b.String()
}

// buildAssistant renders an assistant block: committed glamour prefix + raw
// pending tail while streaming; cached glamour when done. M5d.
func (s *scrollback) buildAssistant(bl *block, width int) string {
	if !bl.done {
		cl := commitLenFor(bl.text)
		// M5d: only (re-)render the committed prefix when there is one (cl > 0)
		// and it advanced or width changed. Rendering an empty prefix would
		// store glamour's non-empty output for "" (a blank paragraph) — wasting
		// a line and masking the done-branch re-render below.
		if cl > 0 && (cl > bl.commitLen || bl.fmtWidth != width) {
			bl.committed = RenderMarkdown(bl.text[:cl], width)
			bl.commitLen = cl
			bl.fmtWidth = width
		}
		pending := ""
		if cl < len(bl.text) {
			pending = wrapRaw(bl.text[cl:], width)
		}
		return bl.committed + pending
	}
	// M5d: a done block must render the FULL text. Re-render when the committed
	// prefix doesn't cover the whole text (commitLen < len) or width changed.
	// Don't key on committed=="" — glamour("") is non-empty, so the streaming
	// path may have left a non-empty-but-incomplete committed cache.
	if bl.commitLen < len(bl.text) || bl.fmtWidth != width {
		bl.committed = RenderMarkdown(bl.text, width)
		bl.commitLen = len(bl.text)
		bl.fmtWidth = width
	}
	return bl.committed
}

// buildTool renders a tool block collapsed (summary line) or expanded (full
// output + diff inlined). M5d. selected highlights the bullet (Task 5 wiring).
func (s *scrollback) buildTool(bl *block, width int, selected bool) string {
	var b strings.Builder
	args := toolHeaderArg(bl.toolName, bl.toolIn)
	marker := "● "
	style := toolStyle
	if selected {
		marker = "▶ "
		style = selectedToolStyle
	}
	b.WriteString("\n" + style.Render(marker+bl.toolName+"("+args+")"))
	if !bl.done {
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString("\n  ↳ " + bl.summary + " " + stateMark(bl.toolState))
	if !bl.expanded {
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString("   (e to collapse)\n")
	if bl.diff != "" {
		b.WriteString(indentBlock(renderDiff(bl.diff, width-2, curTheme), "  ") + "\n")
	}
	if out := strings.TrimSpace(bl.toolOut); out != "" {
		if bl.toolName == "Read" {
			if fn := filenameFromToolInput(bl.toolIn); fn != "" {
				out = highlightCode(out, fn, curTheme)
			}
		}
		b.WriteString(indentBlock(wrapRaw(out, width-2), "  ") + "\n")
	}
	return b.String()
}

// indentBlock prefixes every non-empty line of s with prefix. M5d.
func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = prefix + ln
		}
	}
	return strings.Join(lines, "\n")
}

// stateMark renders a colored state marker for a finished tool call.
func stateMark(state string) string {
	switch state {
	case "success":
		return successStyle.Render("[✓]")
	case "error":
		return errorStyle.Render("[✗]")
	case "denied":
		return warnStyle.Render("[⊘]")
	default:
		return "[" + state + "]"
	}
}
