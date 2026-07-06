package config

import "testing"

func TestLoadFromEnvAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "anthropic" || cfg.APIKey != "sk-test" {
		t.Fatalf("got provider=%s key=%s", cfg.Provider, cfg.APIKey)
	}
	if cfg.Model == "" {
		t.Fatal("expected default model")
	}
	if cfg.Permission != "accept_edits" || cfg.Output != OutputText || cfg.MaxIters != 50 {
		t.Fatalf("defaults wrong: perm=%s out=%s iters=%d", cfg.Permission, cfg.Output, cfg.MaxIters)
	}
}

func TestLoadFlagOverrides(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	cfg, err := Load(Flags{
		Provider: "openai", APIKey: "k", Model: "gpt-4o",
		Output: "stream-json", MaxIters: 5, Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "openai" || cfg.APIKey != "k" || cfg.Model != "gpt-4o" {
		t.Fatalf("got %+v", cfg)
	}
	if cfg.Output != OutputStreamJSON || cfg.MaxIters != 5 {
		t.Fatalf("overrides wrong: %+v", cfg)
	}
}

func TestLoadNoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	if _, err := Load(Flags{Prompt: "hi"}); err == nil {
		t.Fatal("expected error when no API key")
	}
}

func TestLoadResumeContinuePassThrough(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", Resume: "sess-123"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resume != "sess-123" {
		t.Fatalf("resume: %s", cfg.Resume)
	}
	cfg2, err := Load(Flags{Prompt: "hi", Continue: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg2.Continue {
		t.Fatal("continue not set")
	}
}

func TestLoadSandboxPassThrough(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("E2B_API_KEY", "")
	cfg, err := Load(Flags{Provider: "openai", APIKey: "k", Sandbox: "docker"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox != "docker" {
		t.Fatalf("sandbox: %q", cfg.Sandbox)
	}
	cfg2, err := Load(Flags{Provider: "openai", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Sandbox != "" {
		t.Fatalf("default sandbox should be empty: %q", cfg2.Sandbox)
	}
}

func TestLoadOllamaDefaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	cfg, err := Load(Flags{Provider: "ollama", Model: "qwen2.5-coder"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "ollama" || cfg.BaseURL != "http://localhost:11434" || cfg.APIKey != "ollama" || cfg.Model != "qwen2.5-coder" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadOllamaOverrides(t *testing.T) {
	cfg, err := Load(Flags{Provider: "ollama", Model: "m", BaseURL: "http://x:1234", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://x:1234" || cfg.APIKey != "k" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

// TestLoadThinkingFlag — M7a: --thinking / --thinking-budget resolve into
// cfg.Thinking / cfg.ThinkingBudget. Budget defaults to 4096 when thinking is
// enabled but no explicit budget is passed (matches Anthropic'"'"'s starter).
func TestLoadThinkingFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", Thinking: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Thinking {
		t.Fatal("cfg.Thinking should be true")
	}
	if cfg.ThinkingBudget != 4096 {
		t.Fatalf("default ThinkingBudget = %d, want 4096", cfg.ThinkingBudget)
	}
	cfg2, err := Load(Flags{Prompt: "hi", Thinking: true, ThinkingBudget: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.ThinkingBudget != 8000 {
		t.Fatalf("explicit budget = %d, want 8000", cfg2.ThinkingBudget)
	}
}

// TestLoadThinkingEnv — LATHE_THINKING=1 / LATHE_THINKING_BUDGET=N without
// flags.
func TestLoadThinkingEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_THINKING", "1")
	t.Setenv("LATHE_THINKING_BUDGET", "12000")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Thinking || cfg.ThinkingBudget != 12000 {
		t.Fatalf("env: thinking=%v budget=%d", cfg.Thinking, cfg.ThinkingBudget)
	}
}

// TestLoadThinkingDisabledByDefault — with no flag or env, thinking is off.
func TestLoadThinkingDisabledByDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_THINKING", "")
	t.Setenv("LATHE_THINKING_BUDGET", "")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thinking {
		t.Fatalf("Thinking should default to false")
	}
	if cfg.ThinkingBudget != 0 {
		t.Fatalf("ThinkingBudget should default to 0 when off: %d", cfg.ThinkingBudget)
	}
}

// TestLoadEffortFlag — M7b: --effort resolves into cfg.ReasoningEffort. Empty
// means "provider default"; only the OpenAI provider currently honors it.
func TestLoadEffortFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "high" {
		t.Fatalf("effort = %q, want high", cfg.ReasoningEffort)
	}
}

func TestLoadEffortEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_EFFORT", "medium")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "medium" {
		t.Fatalf("effort = %q, want medium", cfg.ReasoningEffort)
	}
}

func TestLoadEffortDefaultEmpty(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_EFFORT", "")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "" {
		t.Fatalf("effort default = %q, want empty", cfg.ReasoningEffort)
	}
}

// TestLoadJailFlag — M7f: --jail sets cfg.Jail. Default is off so existing
// scripts / tests that read paths outside cwd keep working; users opt in.
func TestLoadJailFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", Jail: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Jail {
		t.Fatal("Jail flag not propagated")
	}
}

func TestLoadJailEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_JAIL", "1")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Jail {
		t.Fatal("LATHE_JAIL=1 not honored")
	}
}

func TestLoadJailDefaultOff(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_JAIL", "")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jail {
		t.Fatal("Jail should default off")
	}
}

// TestLoadPromptCachingFlag — M8a: --prompt-caching / LATHE_PROMPT_CACHING
// resolve into cfg.PromptCaching. Off by default; only takes effect against
// the Anthropic provider.
func TestLoadPromptCachingFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", PromptCaching: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PromptCaching {
		t.Fatal("--prompt-caching not propagated")
	}
}

func TestLoadPromptCachingEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_PROMPT_CACHING", "1")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PromptCaching {
		t.Fatal("LATHE_PROMPT_CACHING=1 not honored")
	}
}

func TestLoadPromptCachingDefaultOff(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("LATHE_PROMPT_CACHING", "")
	cfg, err := Load(Flags{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PromptCaching {
		t.Fatal("PromptCaching should default off")
	}
}

// TestLoadCompactRatioFlag — M9d: --compact-ratio and --context-size flow
// through into cfg, letting users tune auto-compact without editing code.
func TestLoadCompactRatioFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", CompactRatio: 0.6, ContextSize: 200000})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompactRatio != 0.6 {
		t.Fatalf("CompactRatio = %v, want 0.6", cfg.CompactRatio)
	}
	if cfg.ContextSize != 200000 {
		t.Fatalf("ContextSize = %d, want 200000", cfg.ContextSize)
	}
}

func TestLoadCompactRatioClamps(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	cfg, err := Load(Flags{Prompt: "hi", CompactRatio: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompactRatio != 0.95 {
		t.Fatalf("above 1.0 should clamp to 0.95, got %v", cfg.CompactRatio)
	}
	cfg2, err := Load(Flags{Prompt: "hi", CompactRatio: -0.5})
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.CompactRatio != 0 {
		t.Fatalf("negative should clamp to 0 (disabled), got %v", cfg2.CompactRatio)
	}
}

func TestLoadOllamaMissingModel(t *testing.T) {
	cfg, err := Load(Flags{Provider: "ollama"})
	if err != nil {
		t.Fatalf("config should not error on missing model (buildChatModel does): %v", err)
	}
	if cfg.Model != "" {
		t.Fatalf("model should be empty: %q", cfg.Model)
	}
	if cfg.APIKey != "ollama" {
		t.Fatalf("dummy key: %q", cfg.APIKey)
	}
}
