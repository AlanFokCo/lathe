package tui

import (
	"context"
	"io"
	"os"

	"github.com/alanfokco/lathe/internal/agent"
	"github.com/alanfokco/lathe/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sirupsen/logrus"
)

// Run builds the engine from cfg and starts the interactive TUI.
func Run(ctx context.Context, cfg *config.Config) error {
	eng, err := agent.NewEngine(ctx, cfg)
	if err != nil {
		return err
	}
	defer eng.Close()
	eng.SetInteractive(true)

	// M13b: silence logrus in alt-screen mode so error lines do not leak
	// below the TUI. Errors are surfaced via the event stream instead.
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(os.Stderr)

	m := newModel(eng, cfg)
	// M8b: enable mouse-cell motion so viewport wheel-scroll and click work.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
