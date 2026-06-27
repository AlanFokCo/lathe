package tui

import (
	"github.com/alanfokco/lathe/internal/agent"
	"github.com/alanfokco/lathe/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// Run builds the engine from cfg and starts the interactive TUI.
func Run(cfg *config.Config) error {
	eng, err := agent.NewEngine(cfg)
	if err != nil {
		return err
	}
	m := newModel(eng, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
