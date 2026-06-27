// Package cli is the print-mode driver: flags → config → engine → render.
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/alanfokco/lathe/internal/agent"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/render"
)

// RunPrint runs non-interactive print mode. Returns the process exit code.
func RunPrint(ctx context.Context, cfg *config.Config) int {
	eng, err := agent.NewEngine(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer eng.Close()
	evCh := eng.Run(ctx, cfg.Prompt)
	switch cfg.Output {
	case config.OutputStreamJSON:
		render.RenderStreamJSON(ctx, evCh, os.Stdout)
	default:
		render.RenderText(ctx, evCh, os.Stdout, os.Stderr)
	}
	return 0
}
