package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStubModelViewAndQuit(t *testing.T) {
	m := newStubModel()
	if got := m.View(); got != "lathe" {
		t.Fatalf("view: got %q", got)
	}
	// pressing 'q' returns a non-nil Cmd (tea.Quit) that terminates the program.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected a non-nil cmd on 'q' (tea.Quit)")
	}
}
