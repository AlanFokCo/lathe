// Package config resolves lathe's runtime configuration: provider, model,
// API key, permission mode, output format. Resolution order: flag > env
// (credential.FromEnv) > defaults. (TOML file loading is a later refinement.)
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/credential"
)

// Version is the lathe version surfaced to the statusline payload. Wire to
// ldflags / debug.ReadBuildInfo before publishing.
const Version = "0.1.0-dev"

// Output is the print-mode output format.
type Output string

const (
	OutputText       Output = "text"
	OutputStreamJSON Output = "stream-json"
)

// Config is the resolved runtime configuration.
type Config struct {
	Provider   string // anthropic | openai | dashscope
	Model      string
	APIKey     string
	BaseURL    string
	Permission string // default | accept_edits | explore | bypass | dont_ask
	Output     Output
	MaxIters   int
	Prompt     string
	Resume     string
	Continue   bool
	Sandbox    string // "" | docker | e2b
	// M6a: resilience knobs for the model wrapper (circuit breaker + rate limit).
	CircuitBreakerThreshold int     // consecutive failures before the breaker opens (default 5)
	RateLimitPerSec         float64 // model calls/sec (0 = unlimited)
	RateBurst               int     // burst allowance for the rate limiter
	Theme                   string  // M6b: TUI theme name (lathe-dark | light)
	Thinking                bool    // M7a: enable Anthropic extended thinking
	ThinkingBudget          int     // M7a: thinking token budget (0 = provider default)
	ReasoningEffort         string  // M7b: OpenAI reasoning effort ("low"|"medium"|"high"); "" = provider default
	Jail                    bool    // M7f: confine file tools to cwd via tool.WithWorkspaceRoot
	PromptCaching           bool    // M8a: enable Anthropic prompt caching (system + tools)
	CompactRatio            float64 // M9d: fraction of context that triggers auto-compact (0 = disabled)
	ContextSize             int     // M9d: model context window override in tokens (0 = model-card default)
	Notify                  bool    // M10f: OSC-9 notification + bell on turn end
}

// Flags holds CLI overrides; empty fields are unset.
type Flags struct {
	Provider, Model, APIKey, BaseURL, Permission, Output, Prompt, Sandbox, Theme string
	MaxIters                                                                     int
	Resume                                                                       string
	Continue                                                                     bool
	Thinking                                                                     bool    // M7a
	ThinkingBudget                                                               int     // M7a
	Effort                                                                       string  // M7b: reasoning effort
	Jail                                                                         bool    // M7f: --jail
	PromptCaching                                                                bool    // M8a: --prompt-caching
	CompactRatio                                                                 float64 // M9d: --compact-ratio
	ContextSize                                                                  int     // M9d: --context-size
	Notify                                                                       bool    // M10f: --notify
}

// Load resolves a Config from flags + env + TOML + defaults (M9e). Resolution
// order: flag > env > TOML (~/.lathe/config.toml) > defaults.
func Load(f Flags) (*Config, error) {
	// M9e: fill empty flag fields from ~/.lathe/config.toml first, so the
	// downstream flag/env resolution still gets to override anything the
	// user set explicitly.
	if t, terr := LoadTOML(DefaultTOMLPath()); terr != nil {
		return nil, terr
	} else {
		f = mergeTOMLIntoFlags(f, t)
	}
	cfg := &Config{
		Permission:              "accept_edits",
		Output:                  OutputText,
		MaxIters:                50,
		Prompt:                  f.Prompt,
		Resume:                  f.Resume,
		Continue:                f.Continue,
		Sandbox:                 f.Sandbox,
		CircuitBreakerThreshold: 5,            // M6a: default breaker threshold
		Theme:                   "lathe-dark", // M6b: default theme
	}
	if f.Theme != "" {
		cfg.Theme = f.Theme
	} else if e := os.Getenv("LATHE_THEME"); e != "" {
		cfg.Theme = e
	}
	if f.Permission != "" {
		cfg.Permission = f.Permission
	}
	if f.Output != "" {
		cfg.Output = Output(f.Output)
	}
	if f.MaxIters > 0 {
		cfg.MaxIters = f.MaxIters
	}
	// M7a: extended thinking — flag > env > off. Budget defaults to 4096
	// tokens when thinking is enabled but no explicit budget is given
	// (Anthropic's minimum useful budget; leaves plenty of room in an 8k
	// max-output-tokens envelope).
	cfg.Thinking = f.Thinking
	cfg.ThinkingBudget = f.ThinkingBudget
	if !cfg.Thinking {
		if envTruthy(os.Getenv("LATHE_THINKING")) {
			cfg.Thinking = true
		}
	}
	if cfg.Thinking && cfg.ThinkingBudget == 0 {
		if e := os.Getenv("LATHE_THINKING_BUDGET"); e != "" {
			if n, err := strconv.Atoi(e); err == nil && n > 0 {
				cfg.ThinkingBudget = n
			}
		}
		if cfg.ThinkingBudget == 0 {
			cfg.ThinkingBudget = 4096
		}
	}
	// M7b: reasoning effort. Flag > env; empty leaves provider defaults.
	cfg.ReasoningEffort = f.Effort
	if cfg.ReasoningEffort == "" {
		cfg.ReasoningEffort = os.Getenv("LATHE_EFFORT")
	}
	// M7f: workspace-root jail. Flag > env; default off so existing scripts
	// that touch files outside cwd keep working (users opt in for hardening).
	cfg.Jail = f.Jail
	if !cfg.Jail && envTruthy(os.Getenv("LATHE_JAIL")) {
		cfg.Jail = true
	}
	// M8a: Anthropic prompt caching. Flag > env; default off so wire format
	// stays byte-identical for callers who do not opt in. Non-Anthropic
	// providers ignore the flag (buildChatModel only threads it into the
	// AnthropicConfig).
	cfg.PromptCaching = f.PromptCaching
	if !cfg.PromptCaching && envTruthy(os.Getenv("LATHE_PROMPT_CACHING")) {
		cfg.PromptCaching = true
	}
	// M9d: auto-compact tuning. Ratio clamps to [0, 0.95] so a stray 1.0
	// setting cannot disable compression by never triggering. Negative
	// values disable it entirely (0). ContextSize is passed through raw;
	// the engine falls back to the model card when it is 0.
	cfg.CompactRatio = f.CompactRatio
	if cfg.CompactRatio < 0 {
		cfg.CompactRatio = 0
	}
	if cfg.CompactRatio > 0.95 {
		cfg.CompactRatio = 0.95
	}
	cfg.ContextSize = f.ContextSize
	// M10f: OSC-9 notification. Flag > env; default off.
	cfg.Notify = f.Notify
	if !cfg.Notify && envTruthy(os.Getenv("LATHE_NOTIFY")) {
		cfg.Notify = true
	}

	if f.Provider != "" {
		cfg.Provider = f.Provider
		cfg.BaseURL = f.BaseURL
		cfg.APIKey = f.APIKey
		if cfg.APIKey == "" {
			cfg.APIKey = os.Getenv(envKeyFor(cfg.Provider))
		}
		cfg.Model = pickDefaultModel(cfg.Provider, f.Model)
	} else {
		cred := credential.FromEnv()
		if cred == nil {
			return nil, fmt.Errorf("no API key: set ANTHROPIC_API_KEY / OPENAI_API_KEY / DASHSCOPE_API_KEY or pass --provider/--api-key")
		}
		cfg.Provider = cred.Provider()
		cfg.APIKey = cred.APIKey()
		cfg.BaseURL = cred.BaseURL()
		cfg.Model = pickDefaultModel(cfg.Provider, f.Model)
	}
	// M5a: ollama reuses the OpenAI client against a local OpenAI-compatible
	// endpoint. Ollama needs no key, but the OpenAI client requires a non-empty
	// APIKey, so default to a dummy. base-url defaults to the standard Ollama port.
	if cfg.Provider == "ollama" {
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:11434"
		}
		if cfg.APIKey == "" {
			cfg.APIKey = "ollama"
		}
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key for provider %q", cfg.Provider)
	}
	return cfg, nil
}

func pickDefaultModel(provider, override string) string {
	if override != "" {
		return override
	}
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-20250514"
	case "openai":
		return "gpt-4o-mini"
	case "dashscope":
		return "qwen-plus"
	}
	return override
}

// envTruthy is the same tri-value convention used by Docker/git tooling:
// "1"/"true"/"yes"/"on" enable, everything else (including empty) is off.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func envKeyFor(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "dashscope":
		return "DASHSCOPE_API_KEY"
	}
	return ""
}
