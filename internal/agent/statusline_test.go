package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
)

func TestEngineStatusInfo(t *testing.T) {
	eng := &Engine{
		cwd:         "/work",
		session:     &session.Session{ID: "abc", Path: "/home/.lathe/projects/x/abc.jsonl"},
		compressCfg: compressConfig{ContextSize: 128000},
	}
	cwd, sid, tp, cs := eng.StatusInfo()
	if cwd != "/work" || sid != "abc" || tp != "/home/.lathe/projects/x/abc.jsonl" || cs != 128000 {
		t.Fatalf("StatusInfo: %q %q %q %d", cwd, sid, tp, cs)
	}
}

func TestEngineStatusInfoNilSession(t *testing.T) {
	eng := &Engine{cwd: "/w"} // session nil, compressCfg zero
	cwd, sid, tp, cs := eng.StatusInfo()
	if cwd != "/w" || sid != "" || tp != "" || cs != 0 {
		t.Fatalf("nil session StatusInfo: %q %q %q %d", cwd, sid, tp, cs)
	}
}

func TestEngineStatusLineConfig(t *testing.T) {
	sl := &settings.StatusLineConfig{Type: "command", Command: "echo hi", Padding: 2}
	eng := &Engine{settings: &settings.Settings{StatusLine: sl}}
	got := eng.StatusLineConfig()
	if got == nil || got.Command != "echo hi" || got.Padding != 2 {
		t.Fatalf("StatusLineConfig: %+v", got)
	}
}

func TestEngineStatusLineConfigNil(t *testing.T) {
	eng := &Engine{} // settings nil
	if got := eng.StatusLineConfig(); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestNewEngineExposesStatusLineAndInfo(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj")
	mustMkdir(t, work)
	writeSettingsFile(t, filepath.Join(work, ".lathe", "settings.json"), map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "echo hi", "padding": 1},
	})
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "bypass", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	sl := eng.StatusLineConfig()
	if sl == nil || sl.Command != "echo hi" || sl.Padding != 1 {
		t.Fatalf("statusline not wired from settings: %+v", sl)
	}
	cwd, sid, _, _ := eng.StatusInfo()
	if cwd != work {
		t.Fatalf("cwd: %q", cwd)
	}
	if sid == "" {
		t.Fatal("session id empty")
	}
}
