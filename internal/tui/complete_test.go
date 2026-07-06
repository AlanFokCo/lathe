package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPaletteItems(t *testing.T) {
	if paletteItems("hello") != nil {
		t.Fatal("non-slash input should not open the palette")
	}
	if paletteItems("/theme light") != nil {
		t.Fatal("input with a space should not open the palette")
	}
	if len(paletteItems("/")) == 0 {
		t.Fatal("/ should list all commands")
	}
	names := map[string]bool{}
	for _, c := range paletteItems("/c") {
		names[c.name] = true
	}
	if !names["clear"] || !names["compact"] || !names["config"] {
		t.Fatalf("/c should match clear/compact/config, got %v", names)
	}
	if names["help"] {
		t.Fatal("/c should not match help")
	}
}

func TestPaletteTabCompletes(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.input.SetValue("/c")                  // clear, compact, config
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor 0 -> 1 (compact)
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "/compact " {
		t.Fatalf("Tab should complete to %q, got %q", "/compact ", got)
	}
}

func TestPaletteEnterRunsSelected(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.input.SetValue("/help")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input.Value() != "" {
		t.Fatalf("Enter should reset input, got %q", m.input.Value())
	}
	if !strings.Contains(m.View(), "commands:") {
		t.Fatalf("Enter on /help should run it:\n%s", m.View())
	}
}

func TestPaletteEscDismisses(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.input.SetValue("/the")
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.input.Value() != "" {
		t.Fatalf("Esc should clear the input, got %q", m.input.Value())
	}
}
