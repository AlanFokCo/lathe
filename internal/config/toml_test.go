package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadTOMLMissingFileIsSilent — an absent config file is a common case;
// LoadTOML must NOT surface an error for it (users without a config get
// pure defaults).
func TestLoadTOMLMissingFileIsSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := LoadTOML(DefaultTOMLPath())
	if err != nil {
		t.Fatalf("missing file surfaced error: %v", err)
	}
	if got.Loaded {
		t.Fatalf("Loaded should be false for missing file: %+v", got)
	}
}

func TestLoadTOMLParsesFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `provider = "anthropic"
model = "claude-sonnet-4-6"
permission = "accept_edits"
theme = "light"
max_iters = 42
sandbox = "docker"
thinking = true
thinking_budget = 5000
effort = "high"
jail = true
prompt_caching = true
compact_ratio = 0.6
context_size = 128000
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTOML(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Loaded {
		t.Fatal("Loaded should be true")
	}
	if got.Provider != "anthropic" || got.Model != "claude-sonnet-4-6" {
		t.Fatalf("basic fields: %+v", got)
	}
	if got.Thinking != true || got.ThinkingBudget != 5000 {
		t.Fatalf("thinking: %+v", got)
	}
	if got.Jail != true || got.PromptCaching != true {
		t.Fatalf("bools: %+v", got)
	}
	if got.CompactRatio != 0.6 || got.ContextSize != 128000 {
		t.Fatalf("numbers: %+v", got)
	}
}

// TestLoadTOMLParseError — malformed TOML surfaces an error so a typo cannot
// hide behind silent-default fallback.
func TestLoadTOMLParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("this is not toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTOML(path); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestFlagOverridesTOML — flag > TOML: an explicit flag value wins.
func TestFlagOverridesTOML(t *testing.T) {
	f := Flags{Provider: "openai", Thinking: false}
	tomlValues := TOMLFile{Loaded: true, Provider: "anthropic", Thinking: true, Effort: "high"}
	got := mergeTOMLIntoFlags(f, tomlValues)
	if got.Provider != "openai" {
		t.Fatalf("flag should win, got %q", got.Provider)
	}
	// Empty-flag fields still take from TOML.
	if got.Effort != "high" {
		t.Fatalf("empty flag should take from TOML, got %q", got.Effort)
	}
}

// TestEndToEndTOMLWithLoad — a TOML-only config resolves through Load into a
// working *Config. Provider must come from TOML because Flag+env are empty.
func TestEndToEndTOMLWithLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	dir := filepath.Join(home, ".lathe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "openai"
model = "gpt-4o-mini"
api_key = "sk-from-toml"
theme = "light"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Flags{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4o-mini" || cfg.APIKey != "sk-from-toml" {
		t.Fatalf("cfg from TOML: %+v", cfg)
	}
	if cfg.Theme != "light" {
		t.Fatalf("theme from TOML: %q", cfg.Theme)
	}
}
