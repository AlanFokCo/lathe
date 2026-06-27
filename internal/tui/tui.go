// Package tui is lathe's interactive terminal UI: a bubbletea program that
// consumes the agent.Engine event stream. M2 = pure event-stream consumer.
package tui

import tea "github.com/charmbracelet/bubbletea"

// stubModel is a placeholder until the full model lands in Task 5.
type stubModel struct{}

func newStubModel() stubModel { return stubModel{} }

func (m stubModel) Init() tea.Cmd { return nil }

func (m stubModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "q" {
		return m, tea.Quit
	}
	return m, nil
}

func (m stubModel) View() string { return "lathe" }
