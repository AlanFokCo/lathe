package main

import (
	"fmt"
	"os"

	"github.com/alanfokco/lathe/internal/cli"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/tui"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func newRootCmd() *cobra.Command {
	var prompt, provider, model, apiKey, baseURL, permissionMode, output string
	var maxIters int
	var resumeID string
	var doContinue bool
	var sandbox string

	root := &cobra.Command{
		Use:     "lathe",
		Short:   "lathe — a coding agent CLI",
		Version: version,
	}
	root.Flags().StringVarP(&prompt, "prompt", "p", "", "non-interactive prompt (print mode)")
	root.Flags().StringVar(&provider, "provider", "", "anthropic|openai|dashscope")
	root.Flags().StringVar(&model, "model", "", "model name override")
	root.Flags().StringVar(&apiKey, "api-key", "", "API key override")
	root.Flags().StringVar(&baseURL, "base-url", "", "API base URL override")
	root.Flags().StringVar(&permissionMode, "permission", "accept_edits", "default|accept_edits|explore|bypass|dont_ask")
	root.Flags().StringVar(&output, "output", "text", "text|stream-json")
	root.Flags().IntVar(&maxIters, "max-iters", 50, "max agent iterations")
	root.Flags().StringVar(&resumeID, "resume", "", "resume session <id>")
	root.Flags().BoolVar(&doContinue, "continue", false, "continue most recent session in cwd")
	root.Flags().StringVar(&sandbox, "sandbox", "", "none|docker|e2b (default none = local execution)")

	root.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.Flags{
			Provider: provider, Model: model, APIKey: apiKey, BaseURL: baseURL,
			Permission: permissionMode, Output: output, MaxIters: maxIters, Prompt: prompt,
			Resume: resumeID, Continue: doContinue, Sandbox: sandbox,
		})
		if err != nil {
			return err
		}
		if prompt != "" {
			os.Exit(cli.RunPrint(cmd.Context(), cfg))
		}
		return tui.Run(cmd.Context(), cfg)
	}
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
