package tui

import (
	"fmt"
	"strings"

	"github.com/alanfokco/lathe/internal/event"
	"github.com/charmbracelet/lipgloss"
)

type blockKind int

const (
	kindUser blockKind = iota
	kindAssistant
	kindTool
	kindUsage
	kindError
)

type block struct {
	kind      blockKind
	text      string
	toolName  string
	toolIn    string
	toolOut   string
	toolState string
	diff      string
	done      bool
	formatted string // M5c-2: cached glamour output for assistant blocks
	dirty     bool   // M5c-2: text changed, formatted is stale
}

type scrollback struct {
	blocks        []block
	lastAssistant int
}

func (s *scrollback) appendUser(prompt string) {
	s.blocks = append(s.blocks, block{kind: kindUser, text: prompt})
	s.lastAssistant = -1
}

func (s *scrollback) appendAssistantText(delta string) {
	if s.lastAssistant >= 0 && s.lastAssistant < len(s.blocks) && s.blocks[s.lastAssistant].kind == kindAssistant {
		b := &s.blocks[s.lastAssistant]
		b.text += delta
		b.dirty = true
		b.formatted = ""
		return
	}
	s.blocks = append(s.blocks, block{kind: kindAssistant, text: delta, dirty: true})
	s.lastAssistant = len(s.blocks) - 1
}

// formatPending re-renders the last assistant block if its text changed since
// the last format (M5c-2). Called on the spinner tick (~10fps) for live
// Markdown, and once at turn end for the final state. Cheap for completed
// (cached) blocks.
func (s *scrollback) formatPending(width int) {
	for i := len(s.blocks) - 1; i >= 0; i-- {
		if s.blocks[i].kind == kindAssistant {
			b := &s.blocks[i]
			if b.dirty {
				b.formatted = RenderMarkdown(b.text, width)
				b.dirty = false
			}
			return
		}
	}
}

func (s *scrollback) appendTool(id, name, input string) {
	s.blocks = append(s.blocks, block{kind: kindTool, toolName: name, toolIn: input})
	s.lastAssistant = -1
}

func (s *scrollback) finishTool(id, output, state, diff string) {
	for i := len(s.blocks) - 1; i >= 0; i-- {
		if s.blocks[i].kind == kindTool && !s.blocks[i].done {
			s.blocks[i].toolOut = output
			s.blocks[i].toolState = state
			s.blocks[i].diff = diff
			s.blocks[i].done = true
			return
		}
	}
}

func (s *scrollback) appendUsage(u event.Usage) {
	s.blocks = append(s.blocks, block{
		kind: kindUsage,
		text: fmt.Sprintf("model=%s in=%d out=%d", u.Model, u.InputTokens, u.OutputTokens),
	})
	s.lastAssistant = -1
}

func (s *scrollback) appendError(err error) {
	s.blocks = append(s.blocks, block{kind: kindError, text: err.Error()})
	s.lastAssistant = -1
}

func (s *scrollback) clear() { s.blocks = nil; s.lastAssistant = 0 }

var (
	userStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	toolStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	usageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func (s *scrollback) render(width int) string {
	var b strings.Builder
	_ = width // M2: no wrapping; width reserved for a later refinement.
	for _, bl := range s.blocks {
		switch bl.kind {
		case kindUser:
			b.WriteString(userStyle.Render("> ") + bl.text + "\n")
		case kindAssistant:
			b.WriteString(bl.text)
		case kindTool:
			b.WriteString("\n" + toolStyle.Render("⏺ "+bl.toolName+"("+strings.TrimSpace(bl.toolIn)+")"))
			if bl.done {
				b.WriteString("\n  ↳ " + strings.TrimSpace(bl.toolOut) + " [" + bl.toolState + "]\n")
				if bl.diff != "" {
					b.WriteString(bl.diff + "\n")
				}
			} else {
				b.WriteString("\n")
			}
		case kindUsage:
			b.WriteString(usageStyle.Render("\n[tokens "+bl.text+"]\n"))
		case kindError:
			b.WriteString(errorStyle.Render("\nerror: "+bl.text+"\n"))
		}
	}
	return b.String()
}
