package tui

import tea "github.com/charmbracelet/bubbletea"

// handleVimNormal dispatches a rune pressed in vim normal mode to the
// corresponding cursor/edit action via synthesised key events. Unrecognised
// keys are silently ignored (they must NOT reach the textarea). M10i.
func (m *model) handleVimNormal(r rune) {
	if m.vimPending == 'd' {
		m.vimPending = 0
		if r == 'd' {
			m.input.Reset()
		}
		return
	}

	switch r {
	case 'h':
		m.vimSend(tea.KeyMsg{Type: tea.KeyLeft})
	case 'l':
		m.vimSend(tea.KeyMsg{Type: tea.KeyRight})
	case 'j':
		m.vimSend(tea.KeyMsg{Type: tea.KeyDown})
	case 'k':
		m.vimSend(tea.KeyMsg{Type: tea.KeyUp})
	case 'w':
		m.vimSend(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	case 'b':
		m.vimSend(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	case '0':
		m.vimSend(tea.KeyMsg{Type: tea.KeyHome})
	case '$':
		m.vimSend(tea.KeyMsg{Type: tea.KeyEnd})
	case 'i':
		m.vimNormal = false
	case 'a':
		m.vimNormal = false
		m.vimSend(tea.KeyMsg{Type: tea.KeyRight})
	case 'x':
		m.vimSend(tea.KeyMsg{Type: tea.KeyDelete})
	case 'd':
		m.vimPending = 'd'
	}
}

// vimSend forwards a synthetic key message to the textarea.
func (m *model) vimSend(msg tea.KeyMsg) {
	m.input, _ = m.input.Update(msg)
}
