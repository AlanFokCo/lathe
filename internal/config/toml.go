package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// TOMLFile is the on-disk shape of ~/.lathe/config.toml (M9e). All fields are
// optional; missing keys fall through to env then defaults. Field names use
// snake_case per TOML convention.
type TOMLFile struct {
	Provider       string  `toml:"provider"`
	Model          string  `toml:"model"`
	BaseURL        string  `toml:"base_url"`
	APIKey         string  `toml:"api_key"`
	Permission     string  `toml:"permission"`
	Theme          string  `toml:"theme"`
	MaxIters       int     `toml:"max_iters"`
	Sandbox        string  `toml:"sandbox"`
	Thinking       bool    `toml:"thinking"`
	ThinkingBudget int     `toml:"thinking_budget"`
	Effort         string  `toml:"effort"`
	Jail           bool    `toml:"jail"`
	PromptCaching  bool    `toml:"prompt_caching"`
	CompactRatio   float64 `toml:"compact_ratio"`
	ContextSize    int     `toml:"context_size"`
	// Loaded reports whether the file was found and parsed (informational for
	// /doctor). Not serialized.
	Loaded bool `toml:"-"`
	// Path is the file the values were loaded from (informational).
	Path string `toml:"-"`
}

// DefaultTOMLPath returns ~/.lathe/config.toml, or "" when the home dir is
// unresolvable.
func DefaultTOMLPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lathe", "config.toml")
}

// LoadTOML reads a config TOML file. Returns a zero-value TOMLFile with
// Loaded=false when the file is absent (a common case worth silent-handling).
// A parse error IS surfaced so users notice a typo instead of silently
// running with defaults.
func LoadTOML(path string) (TOMLFile, error) {
	if path == "" {
		return TOMLFile{}, nil
	}
	var t TOMLFile
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return TOMLFile{}, nil
		}
		return TOMLFile{}, fmt.Errorf("config toml: %w", err)
	}
	if err := toml.Unmarshal(data, &t); err != nil {
		return TOMLFile{}, fmt.Errorf("config toml %s: %w", path, err)
	}
	t.Loaded = true
	t.Path = path
	return t, nil
}

// mergeTOMLIntoFlags returns a Flags with TOML values filling gaps in f.
// The precedence rule is flag > env > toml > defaults; here TOML fills only
// where the flag is unset (zero value for the field type). Env is applied
// later by Load (its own env-reading branches).
func mergeTOMLIntoFlags(f Flags, t TOMLFile) Flags {
	if !t.Loaded {
		return f
	}
	if f.Provider == "" {
		f.Provider = t.Provider
	}
	if f.Model == "" {
		f.Model = t.Model
	}
	if f.BaseURL == "" {
		f.BaseURL = t.BaseURL
	}
	if f.APIKey == "" {
		f.APIKey = t.APIKey
	}
	if f.Permission == "" {
		f.Permission = t.Permission
	}
	if f.Theme == "" {
		f.Theme = t.Theme
	}
	if f.MaxIters == 0 {
		f.MaxIters = t.MaxIters
	}
	if f.Sandbox == "" {
		f.Sandbox = t.Sandbox
	}
	if !f.Thinking {
		f.Thinking = t.Thinking
	}
	if f.ThinkingBudget == 0 {
		f.ThinkingBudget = t.ThinkingBudget
	}
	if f.Effort == "" {
		f.Effort = t.Effort
	}
	if !f.Jail {
		f.Jail = t.Jail
	}
	if !f.PromptCaching {
		f.PromptCaching = t.PromptCaching
	}
	if f.CompactRatio == 0 {
		f.CompactRatio = t.CompactRatio
	}
	if f.ContextSize == 0 {
		f.ContextSize = t.ContextSize
	}
	return f
}
