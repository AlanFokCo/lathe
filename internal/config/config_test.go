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
