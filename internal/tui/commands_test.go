package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
)

func TestCommandRegistryHasCoreCommands(t *testing.T) {
	for _, name := range []string{"help", "clear", "compact", "model", "theme", "config", "mcp", "resume", "quit"} {
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

// TestSlashMCPList — M6c-5: /mcp lists configured MCP servers with tool counts.
func TestSlashMCPList(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", mcpServers: []mcpconfig.ServerInfo{
		{Name: "linear", ToolCount: 5},
		{Name: "github", ToolCount: 12},
	}}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/mcp"); !ok {
		t.Fatal("/mcp not recognized")
	}
	got := m.View()
	for _, want := range []string{"linear", "5", "github", "12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/mcp missing %q:\n%s", want, got)
		}
	}
}

// TestSlashMCPEmpty — with no MCP servers, /mcp says so instead of a blank line.
func TestSlashMCPEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if _, ok := m.maybeSlash("/mcp"); !ok {
		t.Fatal("/mcp not recognized")
	}
	if got := m.View(); !strings.Contains(got, "no MCP servers configured") {
		t.Fatalf("/mcp empty missing message:\n%s", got)
	}
}

// TestSlashResumeList — M6c-5: /resume lists historical sessions with id +
// model + first user prompt and hints at the `lathe --resume <id>` command.
func TestSlashResumeList(t *testing.T) {
	now := time.Now()
	ctrl := &fakeControl{model: "gpt-4o", sessions: []session.Summary{
		{ID: "abc12345deadbeef", Model: "gpt-4o", FirstPrompt: "hello world", ModTime: now},
		{ID: "def45678cafef00d", Model: "gpt-4o-mini", FirstPrompt: "later", ModTime: now.Add(-time.Hour)},
	}}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/resume"); !ok {
		t.Fatal("/resume not recognized")
	}
	got := m.View()
	for _, want := range []string{"abc12345", "hello world", "gpt-4o-mini", "lathe --resume"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/resume missing %q:\n%s", want, got)
		}
	}
}

// TestSlashResumeEmpty — no sessions → friendly message, not a bare header.
func TestSlashResumeEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if _, ok := m.maybeSlash("/resume"); !ok {
		t.Fatal("/resume not recognized")
	}
	if got := m.View(); !strings.Contains(got, "no sessions found") {
		t.Fatalf("/resume empty missing message:\n%s", got)
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
