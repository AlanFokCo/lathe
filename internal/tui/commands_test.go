package tui

import (
	"strings"
	"testing"
	"time"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/skill"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/subagent"
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

// TestArrowUpEmptyRecallsHistory — M8b: pressing ↑ with an empty input
// pulls the most recent history entry into the textarea. Subsequent ↑ walks
// back through older entries; ↓ walks forward and clears at the newest edge.
func TestArrowUpEmptyRecallsHistory(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.hist = newHistory(t.TempDir() + "/history")
	m.hist.Append("first")
	m.hist.Append("second")
	m.input.Focus()

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "second" {
		t.Fatalf("↑ recall = %q, want second", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "first" {
		t.Fatalf("↑↑ recall = %q, want first", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.input.Value() != "second" {
		t.Fatalf("↓ forward = %q, want second", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.input.Value() != "" {
		t.Fatalf("↓ past newest should clear, got %q", m.input.Value())
	}
}

// TestArrowUpWithContentIsPassthrough — with content already typed, ↑ must
// reach the textarea (multi-line cursor navigation) not history.
func TestArrowUpWithContentIsPassthrough(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.hist = newHistory(t.TempDir() + "/history")
	m.hist.Append("older")
	m.input.Focus()
	m.input.SetValue("in progress")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "in progress" {
		t.Fatalf("↑ with content should not recall, got %q", m.input.Value())
	}
}

// TestCtrlCFirstThenSecondQuits — M8b: while idle, first Ctrl+C shows a
// confirm hint and stays; a second Ctrl+C within 2s returns tea.Quit.
func TestCtrlCFirstThenSecondQuits(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	_, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd1 != nil {
		if msg := cmd1(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("first Ctrl+C should not quit")
			}
		}
	}
	if !strings.Contains(m.View(), "again to quit") {
		t.Fatalf("view missing confirm hint:\n%s", m.View())
	}
	_, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd2 == nil {
		t.Fatal("second Ctrl+C should return a cmd")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Fatalf("second Ctrl+C should quit, got %T", cmd2())
	}
}

// TestCtrlCWhileRunningCancels — while a turn is running, Ctrl+C cancels
// the turn (same as Esc) rather than triggering the confirm-to-quit path.
func TestCtrlCWhileRunningCancels(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.submit("hi")
	if m.state != stateRunning {
		t.Fatalf("state: %v", m.state)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.ctx == nil || m.ctx.Err() == nil {
		t.Fatal("Ctrl+C while running should cancel the turn's ctx")
	}
}

// TestSlashSkillsList — M8c: /skills lists discovered skills with name + one-
// line description so users can see what the model has access to.
func TestSlashSkillsList(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", skills: []skill.Skill{
		{Name: "diagram", Description: "render ASCII diagrams"},
		{Name: "search", Description: "web search helper"},
	}}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/skills"); !ok {
		t.Fatal("/skills not recognized")
	}
	got := m.View()
	for _, want := range []string{"diagram", "render ASCII", "search", "web search"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/skills missing %q:\n%s", want, got)
		}
	}
}

func TestSlashSkillsEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.maybeSlash("/skills")
	if got := m.View(); !strings.Contains(got, "no skills") {
		t.Fatalf("/skills empty missing message:\n%s", got)
	}
}

// TestSlashHooksList — M8c: /hooks reports the settings.json hooks the
// engine has wired, grouped by event so users can audit their config.
func TestSlashHooksList(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", hooks: map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "Bash", Hooks: []settings.Command{{Type: "command", Command: "echo x"}}}},
		"Stop":       {{Matcher: "", Hooks: []settings.Command{{Type: "command", Command: "notify"}}}},
	}}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/hooks")
	got := m.View()
	for _, want := range []string{"PreToolUse", "Bash", "echo x", "Stop", "notify"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/hooks missing %q:\n%s", want, got)
		}
	}
}

func TestSlashHooksEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.maybeSlash("/hooks")
	if got := m.View(); !strings.Contains(got, "no hooks") {
		t.Fatalf("/hooks empty missing message:\n%s", got)
	}
}

// TestSlashSandboxReports — M7f: /sandbox summarizes cwd, sandbox mode, and
// workspace-root jail so users can audit their safety posture at a glance.
func TestSlashSandboxReports(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", cwd: "/proj/x", sandboxMode: "docker", jailed: true}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/sandbox"); !ok {
		t.Fatal("/sandbox not recognized")
	}
	got := m.View()
	for _, want := range []string{"/proj/x", "docker", "jail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/sandbox missing %q:\n%s", want, got)
		}
	}
}

// TestSlashSandboxDefaults — no sandbox, jail off → clearly say "host" + "off".
func TestSlashSandboxDefaults(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", cwd: "/tmp"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/sandbox")
	got := m.View()
	if !strings.Contains(got, "host") || !strings.Contains(got, "off") {
		t.Fatalf("/sandbox defaults missing:\n%s", got)
	}
}

// TestSlashAgentsList — M7e: /agents lists every subagent the parent has
// dispatched (running or completed) so the user can trace what the Task tool
// has done without inflating the scrollback.
func TestSlashAgentsList(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", subagents: []subagent.SubagentInfo{
		{ID: "s1", Description: "sweep configs", Status: "completed", OutputBytes: 128, Duration: 2 * time.Second},
		{ID: "s2", Description: "run tests", Status: "running"},
	}}
	m := newModel(ctrl, testCfg())
	if _, ok := m.maybeSlash("/agents"); !ok {
		t.Fatal("/agents not recognized")
	}
	got := m.View()
	for _, want := range []string{"sweep configs", "run tests", "completed", "running"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/agents missing %q:\n%s", want, got)
		}
	}
}

func TestSlashAgentsEmpty(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if _, ok := m.maybeSlash("/agents"); !ok {
		t.Fatal("/agents not recognized")
	}
	if got := m.View(); !strings.Contains(got, "no subagents") {
		t.Fatalf("/agents empty missing message:\n%s", got)
	}
}

// TestSlashPlanOnOff — M7g: /plan on|off enters/exits plan mode; empty arg
// reports current state.
func TestSlashPlanOnOff(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/plan on")
	if !ctrl.plan {
		t.Fatal("plan mode not entered")
	}
	m.maybeSlash("/plan off")
	if ctrl.plan {
		t.Fatal("plan mode not exited")
	}
}

// TestStatusLineShowsPlanMarker — the pinned status line must call out plan
// mode so the user always knows they'"'"'re in a read-only planning session.
func TestStatusLineShowsPlanMarker(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", plan: true}
	m := newModel(ctrl, testCfg())
	if !strings.Contains(m.View(), "PLAN") {
		t.Fatalf("view missing PLAN marker:\n%s", m.View())
	}
}

// TestSlashEffort — M7b: /effort <level> sets the reasoning effort; /effort
// with no arg reports the current level; /effort off clears it.
func TestSlashEffort(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/effort high")
	if ctrl.Effort() != "high" {
		t.Fatalf("effort not set: %q", ctrl.Effort())
	}
	m.maybeSlash("/effort")
	if !strings.Contains(m.View(), "high") {
		t.Fatalf("/effort status missing:\n%s", m.View())
	}
	m.maybeSlash("/effort off")
	if ctrl.Effort() != "" {
		t.Fatalf("effort not cleared: %q", ctrl.Effort())
	}
}

func TestSlashEffortRejectsBadLevel(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/effort ludicrous")
	if ctrl.Effort() == "ludicrous" {
		t.Fatal("unknown effort should be rejected, not set verbatim")
	}
	if !strings.Contains(m.View(), "invalid") {
		t.Fatalf("/effort error missing:\n%s", m.View())
	}
}

// TestSlashThinkingOnOff — M7a: /thinking on|off flips the engine flag; /thinking
// with no arg reports current state.
func TestSlashThinkingOnOff(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/thinking on")
	en, _ := ctrl.Thinking()
	if !en {
		t.Fatalf("thinking not enabled: %+v", ctrl.thinkingCalls)
	}
	m.maybeSlash("/thinking off")
	en, _ = ctrl.Thinking()
	if en {
		t.Fatal("thinking not disabled")
	}
	m.maybeSlash("/thinking")
	if !strings.Contains(m.View(), "off") {
		t.Fatalf("/thinking status missing:\n%s", m.View())
	}
}

// TestSlashThinkingBudget — /thinking budget=N (with or without prior "on")
// sets the budget; explicit `/thinking on budget=N` enables at that budget.
func TestSlashThinkingBudget(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/thinking on budget=8000")
	en, bud := ctrl.Thinking()
	if !en || bud != 8000 {
		t.Fatalf("on budget=8000: en=%v bud=%d", en, bud)
	}
}

// TestModelRendersThinkingBlockStyled — M7a: ThinkingBlockDelta events land in
// the scrollback as a thinking block, prefixed and dim-styled so users can see
// the model'"'"'s reasoning without confusing it with the final answer.
func TestModelRendersThinkingBlockStyled(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(asevent.NewThinkingBlockStartEvent("", "th1"))
	m.handleEvent(asevent.NewThinkingBlockDeltaEvent("", "th1", "hmm let me consider"))
	m.handleEvent(asevent.NewThinkingBlockEndEvent("", "th1"))
	got := m.View()
	if !strings.Contains(got, "hmm let me consider") {
		t.Fatalf("view missing thinking body:\n%s", got)
	}
	if !strings.Contains(got, "thinking") {
		t.Fatalf("view missing thinking marker:\n%s", got)
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
