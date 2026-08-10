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
	var prompt, provider, model, apiKey, baseURL, permissionMode, output, themeName string
	var maxIters int
	var resumeID string
	var doContinue bool
	var sandbox string
	var thinking bool
	var thinkingBudget int
	var effort string
	var jail bool
	var promptCaching bool
	var compactRatio float64
	var contextSize int
	var notify bool
	var maxCost float64
	var recordTape, replayTape string

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
	root.Flags().StringVar(&themeName, "theme", "", "TUI theme: lathe-dark|light")
	root.Flags().BoolVar(&thinking, "thinking", false, "enable Anthropic extended thinking (M7a)")
	root.Flags().IntVar(&thinkingBudget, "thinking-budget", 0, "extended-thinking token budget (default 4096 when --thinking)")
	root.Flags().StringVar(&effort, "effort", "", "OpenAI reasoning effort: low|medium|high (M7b)")
	root.Flags().BoolVar(&jail, "jail", false, "confine file tools to cwd (workspace-root jail, M7f)")
	root.Flags().BoolVar(&promptCaching, "prompt-caching", false, "Anthropic prompt caching for system+tools (M8a)")
	root.Flags().Float64Var(&compactRatio, "compact-ratio", 0, "auto-compact when conversation hits this fraction of context (0=default 0.8, M9d)")
	root.Flags().IntVar(&contextSize, "context-size", 0, "override model context window in tokens (0=model card default, M9d)")
	root.Flags().BoolVar(&notify, "notify", false, "OSC-9 notification + bell on turn end (M10f)")
	root.Flags().Float64Var(&maxCost, "max-cost", 0, "spend cap in USD (0=unlimited)")
	root.Flags().StringVar(&recordTape, "record", "", "path to record a replay tape")
	root.Flags().StringVar(&replayTape, "replay", "", "path to replay from a tape")

	root.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.Flags{
			Provider: provider, Model: model, APIKey: apiKey, BaseURL: baseURL,
			Permission: permissionMode, Output: output, MaxIters: maxIters, Prompt: prompt,
			Resume: resumeID, Continue: doContinue, Sandbox: sandbox, Theme: themeName,
			Thinking: thinking, ThinkingBudget: thinkingBudget, Effort: effort,
			Jail: jail, PromptCaching: promptCaching,
			CompactRatio: compactRatio, ContextSize: contextSize, Notify: notify,
			MaxCostUSD: maxCost, RecordTape: recordTape, ReplayTape: replayTape,
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
