package tui

import (
	"strings"
	"testing"
	"time"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandRegistryHasCoreCommands(t *testing.T) {
	for _, name := range []string{"help", "clear", "compact", "model", "theme", "config", "mcp", "resume", "tools", "quit"} {
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

// TestModelTracksTodoFromTaskCreate — M6h: task_create result carries the new
// task as JSON; TUI parses it into a live todo tracker so the pinned checklist
// stays in sync without agentscope needing a dedicated event.
func TestModelTracksTodoFromTaskCreate(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(callStart("t1", "task_create"))
	payload := `{"id":"1","subject":"write tests","description":"...","state":"pending","created_at":"x"}`
	m.handleEvent(asevent.NewToolResultTextDeltaEvent("", "t1", payload))
	m.handleEvent(asevent.NewToolResultEndEvent("", "t1", message.ToolResultSuccess))
	if len(m.todos) != 1 {
		t.Fatalf("todos: %+v", m.todos)
	}
	if m.todos[0].Subject != "write tests" || m.todos[0].State != "pending" {
		t.Fatalf("todo[0] = %+v", m.todos[0])
	}
	if !strings.Contains(m.View(), "write tests") {
		t.Fatalf("view missing pinned todo:\n%s", m.View())
	}
}

// TestModelMergesTodoOnTaskUpdate — subsequent task_update returns the fully
// updated task (same id); the tracker replaces in place, preserving order.
func TestModelMergesTodoOnTaskUpdate(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(callStart("t1", "task_create"))
	m.handleEvent(asevent.NewToolResultTextDeltaEvent("", "t1", `{"id":"1","subject":"a","description":"","state":"pending","created_at":"x"}`))
	m.handleEvent(asevent.NewToolResultEndEvent("", "t1", message.ToolResultSuccess))
	m.handleEvent(callStart("t2", "task_update"))
	m.handleEvent(asevent.NewToolResultTextDeltaEvent("", "t2", `{"id":"1","subject":"a","description":"","state":"completed","created_at":"x"}`))
	m.handleEvent(asevent.NewToolResultEndEvent("", "t2", message.ToolResultSuccess))
	if len(m.todos) != 1 || m.todos[0].State != "completed" {
		t.Fatalf("todos: %+v", m.todos)
	}
}

// TestTodoPaneHiddenWhenEmpty — no tasks → view has no checklist marks.
func TestTodoPaneHiddenWhenEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	got := m.View()
	if strings.Contains(got, "[ ]") || strings.Contains(got, "[x]") {
		t.Fatalf("todo marks should not appear when empty:\n%s", got)
	}
}

// TestSlashToolsList — M6f: /tools lists every tool the engine exposes to the
// model (Bash/Read/Edit/MultiEdit/ApplyPatch/... + MCP + skills + task).
// Empty inventory falls back to a "no tools" message.
func TestSlashToolsList(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", toolNames: []string{"Bash", "Edit", "MultiEdit", "ApplyPatch"}}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/tools"); !ok {
		t.Fatal("/tools not recognized")
	}
	got := m.View()
	for _, want := range []string{"Bash", "Edit", "MultiEdit", "ApplyPatch"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/tools missing %q:\n%s", want, got)
		}
	}
}

func TestSlashToolsEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if _, ok := m.maybeSlash("/tools"); !ok {
		t.Fatal("/tools not recognized")
	}
	if got := m.View(); !strings.Contains(got, "no tools") {
		t.Fatalf("/tools empty missing message:\n%s", got)
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
