package tui

import (
	"context"

	"github.com/alanfokco/lathe/internal/agent"
	"github.com/alanfokco/lathe/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// Run builds the engine from cfg and starts the interactive TUI.
func Run(ctx context.Context, cfg *config.Config) error {
	eng, err := agent.NewEngine(ctx, cfg)
	if err != nil {
		return err
	}
	defer eng.Close()
	eng.SetInteractive(true)
	m := newModel(eng, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
