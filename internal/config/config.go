// Package config resolves lathe's runtime configuration: provider, model,
// API key, permission mode, output format. Resolution order: flag > env
// (credential.FromEnv) > defaults. (TOML file loading is a later refinement.)
package config

import (
	"fmt"
	"os"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/credential"
)

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
}

// Flags holds CLI overrides; empty fields are unset.
type Flags struct {
	Provider, Model, APIKey, BaseURL, Permission, Output, Prompt string
	MaxIters int
	Resume   string
	Continue bool
}

// Load resolves a Config from flags + env + defaults.
func Load(f Flags) (*Config, error) {
	cfg := &Config{
		Permission: "accept_edits",
		Output:     OutputText,
		MaxIters:   50,
		Prompt:     f.Prompt,
		Resume:     f.Resume,
		Continue:   f.Continue,
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
