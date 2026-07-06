package tui

import (
	"strings"
	"testing"
)

func TestCommandRegistryHasCoreCommands(t *testing.T) {
	for _, name := range []string{"help", "clear", "compact", "model", "theme", "config", "quit"} {
		if _, ok := lookupCommand(name); !ok {
			t.Fatalf("registry missing /%s", name)
		}
	}
}

func TestHelpTextGeneratedFromRegistry(t *testing.T) {
	h := helpText()
	for _, want := range []string{"/help", "/theme", "/compact"} {
		if !strings.Contains(h, want) {
			t.Fatalf("help text missing %q:\n%s", want, h)
		}
	}
}

func TestMatchCommandsPrefix(t *testing.T) {
	got := matchCommands("c")
	names := map[string]bool{}
	for _, c := range got {
		names[c.name] = true
	}
	if !names["clear"] || !names["compact"] || !names["config"] {
		t.Fatalf("matchCommands(c) = %v, want clear/compact/config", names)
	}
	if names["help"] {
		t.Fatalf("matchCommands(c) should not include help")
	}
}

func TestSlashThemeSwitchesLive(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg()) // resets curTheme to dark
	if _, ok := m.maybeSlash("/theme light"); !ok {
		t.Fatal("/theme not recognized")
	}
	if curTheme.Name != "light" {
		t.Fatalf("theme not switched, curTheme=%s", curTheme.Name)
	}
	// unknown theme name falls back to dark (theme.Get default)
	m.maybeSlash("/theme nope")
	if curTheme.Name != "lathe-dark" {
		t.Fatalf("unknown theme should default to dark, got %s", curTheme.Name)
	}
}
