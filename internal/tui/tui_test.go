package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/skill"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/subagent"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeRunner provides Run (the streaming channel); fakeControl embeds it and
// adds SetModel/ListModels/ModelName so it satisfies EngineControl.
type fakeRunner struct {
	events []asevent.Event
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) <-chan asevent.Event {
	ch := make(chan asevent.Event, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch
}

type fakeControl struct {
	fakeRunner
	model         string
	models        []string
	sets          []string
	compressCalls int
	approvalCalls []string
	slConfig      *settings.StatusLineConfig
	cwd, sid, tp  string
	ctxSize       int
	mcpServers    []mcpconfig.ServerInfo        // M6c-5
	sessions      []session.Summary             // M6c-5
	toolNames     []string                      // M6f
	thinkingOn    bool                          // M7a
	thinkingBud   int                           // M7a
	thinkingCalls []string                      // M7a: audit trail
	effort        string                        // M7b
	plan          bool                          // M7g
	permMode      string                        // M10c
	subagents     []subagent.SubagentInfo       // M7e
	jailed        bool                          // M7f
	sandboxMode   string                        // M7f
	skills        []skill.Skill                 // M8c
	hooks         map[string][]settings.Matcher // M8c
	agentscopeVer string                        // M9a
}

func (f *fakeControl) SetModel(name string) error {
	f.sets = append(f.sets, name)
	f.model = name
	return nil
}
func (f *fakeControl) ListModels() []string { return f.models }
func (f *fakeControl) ModelName() string    { return f.model }
func (f *fakeControl) CompressNow(ctx context.Context) (string, error) {
	f.compressCalls++
	return "compressed: 10→5 tokens", nil
}
func (f *fakeControl) SubmitApproval(decision string) {
	f.approvalCalls = append(f.approvalCalls, decision)
}

func (f *fakeControl) StatusInfo() (string, string, string, int) {
	return f.cwd, f.sid, f.tp, f.ctxSize
}
func (f *fakeControl) StatusLineConfig() *settings.StatusLineConfig { return f.slConfig }
func (f *fakeControl) MCPServers() []mcpconfig.ServerInfo           { return f.mcpServers }
func (f *fakeControl) ListSessions() []session.Summary              { return f.sessions }
func (f *fakeControl) ToolNames() []string                          { return f.toolNames }
func (f *fakeControl) SetThinking(enable bool, budget int) {
	f.thinkingOn = enable
	if budget > 0 {
		f.thinkingBud = budget
	}
	f.thinkingCalls = append(f.thinkingCalls, fmt.Sprintf("%v/%d", enable, budget))
}
func (f *fakeControl) Thinking() (bool, int)  { return f.thinkingOn, f.thinkingBud }
func (f *fakeControl) SetEffort(level string) { f.effort = level }
func (f *fakeControl) Effort() string         { return f.effort }
func (f *fakeControl) EnterPlanMode()         { f.plan = true }
func (f *fakeControl) ExitPlanMode()          { f.plan = false }
func (f *fakeControl) ApprovePlan()           { f.plan = false; f.permMode = "accept_edits" }
func (f *fakeControl) IsPlanMode() bool       { return f.plan }
func (f *fakeControl) PermissionMode() string {
	if f.permMode == "" {
		return "default"
	}
	return f.permMode
}
func (f *fakeControl) SetPermissionMode(m string)         { f.permMode = m }
func (f *fakeControl) Subagents() []subagent.SubagentInfo { return f.subagents }
func (f *fakeControl) Jailed() bool                       { return f.jailed }
func (f *fakeControl) SandboxMode() string {
	if f.sandboxMode == "" {
		return "host"
	}
	return f.sandboxMode
}
func (f *fakeControl) SkillsList() []skill.Skill                { return f.skills }
func (f *fakeControl) HooksList() map[string][]settings.Matcher { return f.hooks }
func (f *fakeControl) AgentscopeVersion() string                { return f.agentscopeVer }

func testCfg() *config.Config { return &config.Config{Permission: "accept_edits"} }

// event helpers (agentscope constructors need replyID/IDs; "" / "t1" suffice for tests).
func td(delta string) asevent.Event { return asevent.NewTextBlockDeltaEvent("", "", delta) }
func usage(in, out int) asevent.Event {
	return asevent.NewModelCallEndEvent("", in, out)
}
func end() asevent.Event { return asevent.NewReplyEndEvent("", "") }
func callStart(id, name string) asevent.Event {
	return asevent.NewToolCallStartEvent("", id, name)
}
func result(id, name, output string, state message.ToolResultState) []asevent.Event {
	return []asevent.Event{
		asevent.NewToolResultStartEvent("", id, name),
		asevent.NewToolResultTextDeltaEvent("", id, output),
		asevent.NewToolResultEndEvent("", id, state),
	}
}
func confirm(id, name string) asevent.Event {
	return asevent.NewRequireUserConfirmEvent("", []message.ToolCallBlock{
		{Type: "tool_call", ID: id, Name: name},
	})
}

func TestModelRendersStreamingTextTurn(t *testing.T) {
	runner := &fakeControl{model: "gpt-4o", fakeRunner: fakeRunner{events: []asevent.Event{
		td("Hel"),
		td("lo"),
		usage(1, 2),
		end(),
	}}}
	m := newModel(runner, testCfg())
	cmd := m.submit("hi")
	pumpModel(t, m, cmd)

	if m.state != stateIdle {
		t.Fatalf("state: %v", m.state)
	}
	got := m.View()
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "gpt-4o") {
		t.Fatalf("view missing expected content:\n%s", got)
	}
}

func TestModelESCInterruptsRunning(t *testing.T) {
	runner := &fakeControl{model: "gpt-4o"}
	m := newModel(runner, testCfg())
	m.submit("hi")
	if m.state != stateRunning {
		t.Fatalf("state: %v", m.state)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.ctx == nil || m.ctx.Err() == nil {
		t.Fatal("expected ctx canceled after ESC")
	}
}

func TestModelSlashClear(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.sb.appendUser("old line")
	cmd, ok := m.maybeSlash("/clear")
	if !ok || cmd != nil {
		t.Fatalf("maybeSlash(/clear) = (%v,%v)", cmd, ok)
	}
	if got := m.View(); strings.Contains(got, "old line") {
		t.Fatalf("expected scrollback cleared, got %q", got)
	}
}

func TestModelCostAccumulation(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(usage(10, 5))
	m.handleEvent(usage(3, 2))
	if m.cumIn != 13 || m.cumOut != 7 {
		t.Fatalf("cum: in=%d out=%d", m.cumIn, m.cumOut)
	}
	if !strings.Contains(m.View(), "in=13 out=7") {
		t.Fatalf("status line missing cum tokens:\n%s", m.View())
	}
}

func TestModelSlashModelList(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o", models: []string{"gpt-4o", "gpt-4o-mini"}}, testCfg())
	m.maybeSlash("/model")
	got := m.View()
	if !strings.Contains(got, "gpt-4o-mini") || !strings.Contains(got, "current=gpt-4o") {
		t.Fatalf("/model list missing entries:\n%s", got)
	}
}

func TestModelSlashModelSwitch(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", models: []string{"gpt-4o"}}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/model gpt-4o-mini")
	if ctrl.model != "gpt-4o-mini" {
		t.Fatalf("model not switched: %s", ctrl.model)
	}
}

func TestModelSlashConfigRedactsKey(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, &config.Config{
		Provider: "openai", Model: "gpt-4o", APIKey: "sk-secret123456", Permission: "accept_edits", MaxIters: 50,
	})
	m.maybeSlash("/config")
	got := m.View()
	if !strings.Contains(got, "openai") {
		t.Fatalf("/config missing provider:\n%s", got)
	}
	if strings.Contains(got, "secret123456") {
		t.Fatalf("/config leaked full API key:\n%s", got)
	}
	if !strings.Contains(got, "sk-s") {
		t.Fatalf("/config missing redacted key prefix:\n%s", got)
	}
}

// pumpModel drives the model by executing the returned Cmd chain (like bubbletea
// would): cmd() → Msg → Update(Msg) → next Cmd, until a Cmd returns nil.
//
// A tea.BatchMsg is unfolded by following its first sub-cmd (the event pump).
// Concurrent sub-cmds (e.g. the spinner tick from submit) are ignored here —
// the real bubbletea runtime runs them concurrently; this helper is single-chain.
func pumpModel(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			if len(b) == 0 {
				break
			}
			cmd = b[0]
			continue
		}
		var c tea.Cmd
		_, c = m.Update(msg)
		cmd = c
	}
}

func TestModelSlashCompact(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.maybeSlash("/compact")
	if ctrl.compressCalls != 1 {
		t.Fatalf("CompressNow calls: %d", ctrl.compressCalls)
	}
	if !strings.Contains(m.View(), "compressed") {
		t.Fatalf("/compact missing feedback:\n%s", m.View())
	}
}

func TestModelHandleCompactedEvent(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(asevent.NewCustomEvent("", "compacted", map[string]any{"before": 1000, "after": 100}))
	got := m.View()
	if !strings.Contains(got, "1000") || !strings.Contains(got, "100") {
		t.Fatalf("scrollback missing compacted tokens:\n%s", got)
	}
}

func TestModelRequireApprovalShowsModal(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(confirm("t1", "Bash"))
	if m.state != stateAwaitingApproval {
		t.Fatalf("state: %v", m.state)
	}
	got := m.View()
	if !strings.Contains(got, "Bash") || !strings.Contains(got, "[y]") || !strings.Contains(got, "[n]") || !strings.Contains(got, "[a]") {
		t.Fatalf("modal missing content:\n%s", got)
	}
}

func TestModelApprovalKeys(t *testing.T) {
	cases := []struct {
		key  byte
		want string
	}{
		{'y', "allow"},
		{'n', "deny"},
		{'a', "always"},
	}
	for _, c := range cases {
		ctrl := &fakeControl{model: "gpt-4o"}
		m := newModel(ctrl, testCfg())
		m.handleEvent(confirm("t1", "Bash"))
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(c.key)}})
		if len(ctrl.approvalCalls) != 1 || ctrl.approvalCalls[0] != c.want {
			t.Fatalf("key %c: got %v want %q", c.key, ctrl.approvalCalls, c.want)
		}
		if m.state != stateRunning {
			t.Fatalf("state after key: %v", m.state)
		}
	}
}

func TestModelApprovalESC(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.handleEvent(confirm("t1", "Bash"))
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if len(ctrl.approvalCalls) != 1 || ctrl.approvalCalls[0] != "deny" {
		t.Fatalf("ESC: got %v want deny", ctrl.approvalCalls)
	}
}

func TestStatusLineFallback(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(usage(7, 3))
	got := m.View()
	if !strings.Contains(got, "gpt-4o") || !strings.Contains(got, "in=7 out=3") {
		t.Fatalf("fallback status missing:\n%s", got)
	}
	if strings.Contains(got, "model=") {
		t.Fatalf("old model= label should be gone:\n%s", got)
	}
}

func TestWidthFromWindowSizeMsg(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if m.wrapWidth() != 80 {
		t.Fatalf("default wrapWidth: %d", m.wrapWidth())
	}
	m.Update(tea.WindowSizeMsg{Width: 120})
	if m.width != 120 || m.wrapWidth() != 120 {
		t.Fatalf("after WindowSizeMsg: width=%d wrap=%d", m.width, m.wrapWidth())
	}
}

func TestStatusLineRendersCommandOutput(t *testing.T) {
	ctrl := &fakeControl{
		model:    "gpt-4o",
		slConfig: &settings.StatusLineConfig{Type: "command", Command: "echo OK"},
		cwd:      t.TempDir(), sid: "s1", tp: "/p/s1.jsonl", ctxSize: 128000,
	}
	m := newModel(ctrl, testCfg())
	cmd := m.scheduleStatusLine()
	if cmd == nil {
		t.Fatal("nil cmd for configured statusline")
	}
	m.Update(cmd())
	got := m.View()
	if !strings.Contains(got, "OK") {
		t.Fatalf("status missing command output:\n%s", got)
	}
}

func TestStatusLineGenGuard(t *testing.T) {
	ctrl := &fakeControl{
		model:    "gpt-4o",
		slConfig: &settings.StatusLineConfig{Type: "command", Command: "echo first"},
	}
	m := newModel(ctrl, testCfg())
	cmd1 := m.scheduleStatusLine() // gen=1, cfg.Command="echo first"
	ctrl.slConfig.Command = "echo second"
	cmd2 := m.scheduleStatusLine() // gen=2, cfg.Command="echo second"
	m.Update(cmd2())               // applies gen2 → "second"
	m.Update(cmd1())               // gen1 != slGen(2) → discarded
	got := m.View()
	if !strings.Contains(got, "second") {
		t.Fatalf("want second (gen guard):\n%s", got)
	}
	if strings.Contains(got, "first") {
		t.Fatalf("stale gen1 leaked:\n%s", got)
	}
}

func TestStatusLineRefreshesAfterReplyEnd(t *testing.T) {
	ctrl := &fakeControl{
		model:    "gpt-4o",
		slConfig: &settings.StatusLineConfig{Type: "command", Command: "echo refreshed"},
		fakeRunner: fakeRunner{events: []asevent.Event{
			usage(5, 1),
			end(),
		}},
	}
	m := newModel(ctrl, testCfg())
	pumpModel(t, m, m.submit("hi"))
	got := m.View()
	if !strings.Contains(got, "refreshed") {
		t.Fatalf("status not refreshed after ReplyEnd:\n%s", got)
	}
}

func TestStatusLineRefreshesOnModelSwitch(t *testing.T) {
	ctrl := &fakeControl{
		model:    "gpt-4o",
		slConfig: &settings.StatusLineConfig{Type: "command", Command: "echo switched"},
	}
	m := newModel(ctrl, testCfg())
	cmd, ok := m.maybeSlash("/model gpt-4o-mini")
	if !ok || cmd == nil {
		t.Fatalf("maybeSlash /model: (%v, %v)", cmd, ok)
	}
	pumpModel(t, m, cmd)
	got := m.View()
	if !strings.Contains(got, "switched") {
		t.Fatalf("status not refreshed on /model switch:\n%s", got)
	}
	if !strings.Contains(got, "switched to gpt-4o-mini") {
		t.Fatalf("scrollback missing switch message:\n%s", got)
	}
}

// TestActivityLineThinking — M6a Commit B: phase is driven by ModelCallStart
// (no TurnStep/step display).
func TestActivityLineThinking(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(asevent.NewModelCallStartEvent("", "gpt-4o"))
	got := m.View()
	if !strings.Contains(got, "thinking") {
		t.Fatalf("activity line missing thinking:\n%s", got)
	}
}

func TestActivityLineRunning(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(callStart("t1", "Bash"))
	got := m.View()
	if !strings.Contains(got, "running Bash") {
		t.Fatalf("activity line missing running tool:\n%s", got)
	}
}

func TestActivityLineHiddenWhenIdle(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg()) // stateIdle
	m.handleEvent(asevent.NewModelCallStartEvent("", "gpt-4o"))
	got := m.View()
	if strings.Contains(got, "thinking") || strings.Contains(got, "running") {
		t.Fatalf("activity line should be hidden when idle:\n%s", got)
	}
}

func TestSpinnerTickRunningVsIdle(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	if _, c := m.Update(spinner.TickMsg{}); c == nil {
		t.Fatal("expected next tick cmd while running")
	}
	m.state = stateIdle
	if _, c := m.Update(spinner.TickMsg{}); c != nil {
		t.Fatal("expected nil cmd (stop) while idle")
	}
}

func TestInputNoVerticalPromptLine(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	if m.input.Prompt != "" {
		t.Fatalf("textarea Prompt should be empty (no ┃ line), got %q", m.input.Prompt)
	}
	if m.input.ShowLineNumbers {
		t.Fatal("textarea ShowLineNumbers should be false (no line-number gutter)")
	}
}

// TestBuildFormatsOnBoundary replaces the old TestTickFormatsPendingMarkdown.
// Formatting now happens in build() at a boundary, not on the spinner tick.
func TestBuildFormatsOnBoundary(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(td("**hi**")) // no newline → pending, raw
	if !strings.Contains(m.sb.build(80, -1), "**hi**") {
		t.Fatalf("mid-line should show raw pending: %q", m.sb.build(80, -1))
	}
	m.handleEvent(td("\n")) // boundary → glamour
	if strings.Contains(m.sb.build(80, -1), "**") {
		t.Fatalf("post-boundary should be formatted (no **): %q", m.sb.build(80, -1))
	}
}

// TestSpinnerTickDoesNotBuild — M5d iron rule 2: View()/tick path must not
// run glamour. A spinner tick while streaming must not change committed state.
func TestSpinnerTickDoesNotBuild(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(td("**hi**\n")) // boundary → build commits
	m.rebuild()
	committedBefore := m.sb.blocks[0].committed
	m.Update(spinner.TickMsg{})
	if m.sb.blocks[0].committed != committedBefore {
		t.Fatalf("spinner tick mutated committed (iron rule 2 violation): %q != %q", m.sb.blocks[0].committed, committedBefore)
	}
}

// TestFormatTickRebuildsWhileRunning — the 120ms formatTick drives rebuild
// (and re-glams on boundary) while running; idle stops scheduling it.
func TestFormatTickRebuildsWhileRunning(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(td("**hi**\n"))
	_, c := m.Update(formatTickMsg{})
	if c == nil {
		t.Fatal("expected next formatTick cmd while running")
	}
	m.state = stateIdle
	_, c = m.Update(formatTickMsg{})
	if c != nil {
		t.Fatal("expected nil cmd (stop) while idle")
	}
}

// TestWindowSizeMsgSetsViewportHeight — resize configures viewport dims.
func TestWindowSizeMsgSetsViewportHeight(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.height != 40 || m.viewport.Width != 120 || m.viewport.Height != 37 {
		t.Fatalf("resize: height=%d vp=%dx%d (want 40, 120x37)", m.height, m.viewport.Width, m.viewport.Height)
	}
}

// TestRebuildSticksToBottom — when already at bottom, rebuild re-snaps;
// scrolled up, it does not yank back.
func TestRebuildSticksToBottom(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 30; i++ {
		m.sb.appendUser(strings.Repeat("line", 1) + fmt.Sprintf(" %d", i))
	}
	m.rebuild()
	m.viewport.GotoBottom()
	if !m.viewport.AtBottom() {
		t.Fatal("expected at bottom after GotoBottom")
	}
	// scroll up, rebuild, should NOT snap back
	m.viewport.PageUp()
	atBot := m.viewport.AtBottom()
	m.rebuild()
	if atBot {
		t.Fatal("scrolled-up state should be not-at-bottom")
	}
}

// TestToolBlockCollapsedThenExpanded — M5d: default collapsed, e/Enter expands
// inline, full output appears in the viewport content.
func TestToolBlockCollapsedThenExpanded(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(callStart("t1", "Read"))
	for _, ev := range result("t1", "Read", "secret-output-line1\nline2", message.ToolResultSuccess) {
		m.handleEvent(ev)
	}

	view := m.View()
	if strings.Contains(view, "secret-output-line1") {
		t.Fatalf("collapsed view leaked full output:\n%s", view)
	}
	if !strings.Contains(view, "Read") || !strings.Contains(view, "[✓]") {
		t.Fatalf("collapsed view missing summary:\n%s", view)
	}

	// select + expand
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}) // select last tool
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // expand (input empty)
	view = m.View()
	if !strings.Contains(view, "secret-output-line1") {
		t.Fatalf("expanded view missing full output:\n%s", view)
	}
	// collapse again
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(m.View(), "secret-output-line1") {
		t.Fatalf("re-collapsed view leaked output:\n%s", m.View())
	}
}

// TestSelectionCyclesThroughToolBlocks — ] then [ moves selection.
func TestSelectionCyclesThroughToolBlocks(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(callStart("a", "Read"))
	for _, ev := range result("a", "Read", "a-out", message.ToolResultSuccess) {
		m.handleEvent(ev)
	}
	m.handleEvent(callStart("b", "Bash"))
	for _, ev := range result("b", "Bash", "b-out", message.ToolResultSuccess) {
		m.handleEvent(ev)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}) // select last (b)
	if m.selectedTool < 0 || m.sb.blocks[m.selectedTool].toolName != "Bash" {
		t.Fatalf("] should select Bash, got selectedTool=%d", m.selectedTool)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}}) // prev (a)
	if m.sb.blocks[m.selectedTool].toolName != "Read" {
		t.Fatalf("[ should select Read, got selectedTool=%d", m.selectedTool)
	}
}

// TestPgUpPgDownScrollsViewport — M5d: viewport scroll keys work.
func TestPgUpPgDownScrollsViewport(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 30; i++ {
		m.sbAppendUser(fmt.Sprintf("line %d", i))
	}
	m.viewport.GotoBottom()
	yBefore := m.viewport.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.viewport.YOffset >= yBefore {
		t.Fatalf("PgUp should scroll up: %d -> %d", yBefore, m.viewport.YOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	// at least moved back down somewhat
	if m.viewport.YOffset == 0 && yBefore != 0 {
		t.Fatalf("PgDown should scroll down")
	}
}

// TestExpandEGatedOnEmptyInput — typing 'e' with input non-empty goes to the
// textarea, not expand.
func TestExpandEGatedOnEmptyInput(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.handleEvent(callStart("a", "Read"))
	for _, ev := range result("a", "Read", "out", message.ToolResultSuccess) {
		m.handleEvent(ev)
	}
	m.input.Focus()                                              // M5d: mimic Init() focusing the textarea so typed runes land
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}) // select
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // type 'x' into input
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // 'e' → textarea, not expand
	if m.input.Value() != "xe" {
		t.Fatalf("expected input 'xe', got %q", m.input.Value())
	}
	if m.sb.blocks[m.selectedTool].expanded {
		t.Fatal("e with non-empty input should NOT expand")
	}
}

// TestRedrawThrottleBatchesTextDeltas — M6a: streaming TextDeltas mark the
// scrollback dirty but do not rebuild per token; the 120ms formatTick drains it.
func TestRedrawThrottleBatchesTextDeltas(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // rebuild() once
	base := m.rebuildN
	m.state = stateRunning
	for i := 0; i < 5; i++ {
		m.handleEvent(td("x"))
	}
	if m.rebuildN != base {
		t.Fatalf("text deltas must not rebuild immediately: %d vs base %d", m.rebuildN, base)
	}
	if !m.dirty {
		t.Fatal("text delta should mark dirty")
	}
	m.Update(formatTickMsg{})
	if m.rebuildN != base+1 {
		t.Fatalf("tick should rebuild exactly once: %d vs base %d", m.rebuildN, base)
	}
	if m.dirty {
		t.Fatal("tick should clear dirty")
	}
}

// M10b: /plan approve exits plan mode and switches to accept_edits.
func TestSlashPlanApprove(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())

	// approve when not in plan mode → error message
	m.handlePlan("approve")
	if !strings.Contains(m.sb.build(80, -1), "not in plan mode") {
		t.Fatal("approve outside plan mode should warn")
	}

	// enter plan mode, then approve
	m.handlePlan("on")
	if !ctrl.plan {
		t.Fatal("plan should be on")
	}
	m.handlePlan("approve")
	if ctrl.plan {
		t.Fatal("plan should be off after approve")
	}
	if ctrl.permMode != "accept_edits" {
		t.Fatalf("permission mode after approve should be accept_edits, got %q", ctrl.permMode)
	}
	if !strings.Contains(m.sb.build(80, -1), "approved") {
		t.Fatal("approve should confirm in scrollback")
	}
}

// M10b: /plan on message mentions /plan approve.
func TestSlashPlanOnMentionsApprove(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.handlePlan("on")
	if !strings.Contains(m.sb.build(80, -1), "approve") {
		t.Fatal("/plan on message should mention /plan approve")
	}
}

// M10c: Shift+Tab cycles permission mode: default → accept_edits → plan → default.
func TestShiftTabCyclesMode(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", permMode: "default"}
	m := newModel(ctrl, testCfg())

	m.cyclePermissionMode()
	if ctrl.permMode != "accept_edits" {
		t.Fatalf("step 1: want accept_edits, got %q", ctrl.permMode)
	}

	m.cyclePermissionMode()
	if !ctrl.plan {
		t.Fatal("step 2: should enter plan mode")
	}

	m.cyclePermissionMode()
	if ctrl.plan {
		t.Fatal("step 3: should exit plan mode")
	}
	if ctrl.permMode != "default" {
		t.Fatalf("step 3: want default, got %q", ctrl.permMode)
	}
}

// M10c: permModeLabel returns expected labels.
func TestPermModeLabel(t *testing.T) {
	cases := []struct {
		mode       string
		planActive bool
		want       string
	}{
		{"default", false, "ASK"},
		{"accept_edits", false, "EDITS"},
		{"explore", true, "PLAN"},
		{"bypass", false, "BYPASS"},
		{"dont_ask", false, "AUTO"},
	}
	for _, tc := range cases {
		got := permModeLabel(tc.mode, tc.planActive)
		if !strings.Contains(got, tc.want) {
			t.Errorf("permModeLabel(%q, %v) = %q, want to contain %q", tc.mode, tc.planActive, got, tc.want)
		}
	}
}

// M10d: approval bar shows tool name and args preview.
func TestApprovalBar(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.pendingTool = "Bash"
	m.pendingInput = `{"command":"ls -la"}`
	m.width = 80
	bar := m.approvalBar()
	if !strings.Contains(bar, "Bash") {
		t.Fatal("approval bar should contain tool name")
	}
	if !strings.Contains(bar, "ls -la") {
		t.Fatal("approval bar should preview args")
	}
	if !strings.Contains(bar, "[y]es") {
		t.Fatal("approval bar should show key hints")
	}
}

// M10d: formatToolInput truncates long input.
func TestFormatToolInputTruncation(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := formatToolInput(long, 80)
	runes := []rune(got)
	if len(runes) > 80*3+1 {
		t.Fatalf("should truncate long input, got %d runes", len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("truncated input should end with …")
	}
}

// M10e: highlightMatches wraps matches in reverse-video ANSI.
func TestHighlightMatches(t *testing.T) {
	got := highlightMatches("Hello world, hello again", "hello")
	if !strings.Contains(got, "\x1b[7m") {
		t.Fatal("should contain reverse-video escape")
	}
	if strings.Count(got, "\x1b[7m") != 2 {
		t.Fatalf("should highlight 2 matches, got %d", strings.Count(got, "\x1b[7m"))
	}
}

// M10e: search mode toggles on Ctrl+F.
func TestSearchModeToggle(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.width = 80
	m.height = 24

	if m.searchActive {
		t.Fatal("search should start inactive")
	}

	// Ctrl+F activates
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if !m.searchActive {
		t.Fatal("Ctrl+F should activate search")
	}

	// type a query
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if m.searchQuery != "fo" {
		t.Fatalf("query should be 'fo', got %q", m.searchQuery)
	}

	// backspace
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.searchQuery != "f" {
		t.Fatalf("backspace should remove last char, got %q", m.searchQuery)
	}

	// Esc exits
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEscape})
	if m.searchActive {
		t.Fatal("Esc should deactivate search")
	}
}

// M10e: View shows search bar when active.
func TestSearchBarInView(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.width = 80
	m.height = 24
	m.searchActive = true
	m.searchQuery = "test"
	got := m.View()
	if !strings.Contains(got, "search:") {
		t.Fatal("view should show search bar when active")
	}
	if !strings.Contains(got, "test") {
		t.Fatal("view should show the search query")
	}
}

// M10f: lastAssistantText returns the most recent assistant block text.
func TestLastAssistantText(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	if m.lastAssistantText() != "" {
		t.Fatal("no assistant blocks yet, should be empty")
	}
	m.sb.appendAssistantText("first reply")
	m.sb.finishAssistant()
	m.sb.appendUser("a user prompt")
	m.sb.appendAssistantText("second reply")
	m.sb.finishAssistant()
	if got := m.lastAssistantText(); got != "second reply" {
		t.Fatalf("want 'second reply', got %q", got)
	}
}

// M10f: Ctrl+Y in idle with assistant text shows "(copied to clipboard)".
func TestCtrlYCopy(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o"}
	m := newModel(ctrl, testCfg())
	m.width = 80
	m.height = 24
	m.sb.appendAssistantText("hello world")
	m.sb.finishAssistant()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if !strings.Contains(m.sb.build(80, -1), "copied to clipboard") {
		t.Fatal("Ctrl+Y should confirm copy in scrollback")
	}
}

// M10g: argPaletteItems returns candidates for /model, /theme, /effort, /plan.
func TestArgPaletteItems(t *testing.T) {
	ctrl := &fakeControl{model: "gpt-4o", models: []string{"gpt-4o", "gpt-4o-mini"}}
	m := newModel(ctrl, testCfg())

	// /model space → show all models
	args, cmd := argPaletteItems("/model ", m)
	if cmd != "model" || len(args) != 2 {
		t.Fatalf("want 2 model args, got %d (cmd=%q)", len(args), cmd)
	}

	// /model gpt-4o-m → filter
	args, _ = argPaletteItems("/model gpt-4o-m", m)
	if len(args) != 1 || args[0] != "gpt-4o-mini" {
		t.Fatalf("filtered args: %v", args)
	}

	// /effort → show effort levels
	args, cmd = argPaletteItems("/effort ", m)
	if cmd != "effort" || len(args) < 3 {
		t.Fatalf("effort should have candidates: %v", args)
	}

	// /plan → show on/off/approve
	args, cmd = argPaletteItems("/plan ", m)
	if cmd != "plan" || len(args) != 3 {
		t.Fatalf("plan should have 3 args: %v", args)
	}

	// /clear → no argsFn
	args, _ = argPaletteItems("/clear ", m)
	if len(args) != 0 {
		t.Fatalf("clear should have no args: %v", args)
	}
}

// M10g: renderArgPalette renders a line with candidates.
func TestRenderArgPalette(t *testing.T) {
	got := renderArgPalette([]string{"low", "medium", "high"}, 1, "effort")
	if !strings.Contains(got, "medium") {
		t.Fatal("should contain 'medium'")
	}
	if !strings.Contains(got, "/effort") {
		t.Fatal("should mention the command name")
	}
}

// M10h: paginateOutput truncates long output.
func TestPaginateOutput(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	all := strings.Join(lines, "\n")
	got := paginateOutput(all)
	if !strings.Contains(got, "lines hidden") {
		t.Fatal("should contain hidden-lines notice")
	}
	if !strings.Contains(got, "line 0") {
		t.Fatal("should contain head lines")
	}
	if !strings.Contains(got, "line 299") {
		t.Fatal("should contain tail lines")
	}
	if strings.Contains(got, "line 150") {
		t.Fatal("should NOT contain middle lines")
	}
}

// M10h: paginateOutput passes through short output.
func TestPaginateOutputShort(t *testing.T) {
	short := "line 1\nline 2\nline 3"
	if paginateOutput(short) != short {
		t.Fatal("short output should pass through unchanged")
	}
}

// M10h: diffStat returns +N/-N from unified diff lines.
func TestDiffStat(t *testing.T) {
	diff := "--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,4 @@\n context\n-removed1\n-removed2\n+added1\n+added2\n+added3\n"
	got := diffStat(diff)
	if !strings.Contains(got, "+3") || !strings.Contains(got, "-2") {
		t.Fatalf("diffStat = %q, want +3/-2", got)
	}
}

// M10h: diffStat returns "" for empty diff.
func TestDiffStatEmpty(t *testing.T) {
	if diffStat("") != "" {
		t.Fatal("empty diff should return empty string")
	}
}

// --- M10i: vim mode tests ---

func vimModel() *model {
	m := newModel(&fakeControl{model: "gpt-4o", permMode: "default"}, testCfg())
	m.input.Focus()
	return m
}

func TestVimEscEntersNormalMode(t *testing.T) {
	m := vimModel()
	if m.vimNormal {
		t.Fatal("should start in insert mode")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !m.vimNormal {
		t.Fatal("Esc should enter normal mode")
	}
}

func TestVimIReturnsToInsertMode(t *testing.T) {
	m := vimModel()
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.vimNormal {
		t.Fatal("i should return to insert mode")
	}
}

func TestVimNormalBlocksCharInsertion(t *testing.T) {
	m := vimModel()
	m.input.SetValue("hello")
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if strings.Contains(m.input.Value(), "x") {
		t.Fatalf("normal mode should not insert chars, got %q", m.input.Value())
	}
}

func TestVimHLMovement(t *testing.T) {
	m := vimModel()
	m.input.SetValue("abcdef")
	m.input.CursorEnd()
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	startCol := m.input.LineInfo().CharOffset
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	after := m.input.LineInfo().CharOffset
	if after >= startCol {
		t.Fatalf("h should move left: %d -> %d", startCol, after)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.input.LineInfo().CharOffset != startCol {
		t.Fatalf("l should move right back: %d -> %d", after, m.input.LineInfo().CharOffset)
	}
}

func TestVim0Dollar(t *testing.T) {
	m := vimModel()
	m.input.SetValue("hello world")
	m.input.SetCursor(5)
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if m.input.LineInfo().CharOffset != 0 {
		t.Fatalf("0 should go to start, got offset %d", m.input.LineInfo().CharOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'$'}})
	if m.input.LineInfo().CharOffset == 0 {
		t.Fatal("$ should move to end of line")
	}
}

func TestVimWBWordMotion(t *testing.T) {
	m := vimModel()
	m.input.SetValue("one two three")
	m.input.CursorStart()
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	col := m.input.LineInfo().CharOffset
	if col < 2 {
		t.Fatalf("w should advance past first word, col=%d", col)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	col2 := m.input.LineInfo().CharOffset
	if col2 >= col {
		t.Fatalf("b should move back, col=%d -> %d", col, col2)
	}
}

func TestVimXDeletesChar(t *testing.T) {
	m := vimModel()
	m.input.SetValue("abcdef")
	m.input.SetCursor(2)
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	got := m.input.Value()
	if len(got) != 5 {
		t.Fatalf("x should delete exactly one char: len=%d val=%q", len(got), got)
	}
}

func TestVimDDClearsLine(t *testing.T) {
	m := vimModel()
	m.input.SetValue("delete me")
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.input.Value() != "" {
		t.Fatalf("dd should clear input, got %q", m.input.Value())
	}
}

func TestVimAEntersInsertAndAdvances(t *testing.T) {
	m := vimModel()
	m.input.SetValue("abc")
	m.input.SetCursor(1)
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	before := m.input.LineInfo().CharOffset
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.vimNormal {
		t.Fatal("a should enter insert mode")
	}
	after := m.input.LineInfo().CharOffset
	if after <= before {
		t.Fatalf("a should advance cursor: %d -> %d", before, after)
	}
}

func TestVimModeIndicatorInView(t *testing.T) {
	m := vimModel()
	if strings.Contains(m.View(), "NORMAL") {
		t.Fatal("insert mode should not show NORMAL")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !strings.Contains(m.View(), "NORMAL") {
		t.Fatalf("normal mode should show NORMAL in view:\n%s", m.View())
	}
}

func TestVimEscDuringRunningStillCancels(t *testing.T) {
	m := vimModel()
	m.submit("hi")
	if m.state != stateRunning {
		t.Fatalf("state: %v", m.state)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.ctx == nil || m.ctx.Err() == nil {
		t.Fatal("Esc during running should still cancel engine")
	}
	if m.vimNormal {
		t.Fatal("Esc during running should not enter vim normal mode")
	}
}

// ── M11a tests ──────────────────────────────────────────────────────────

func TestInputWidthFollowsResize(t *testing.T) {
	fc := &fakeControl{model: "test"}
	m := newModel(fc, testCfg())
	m.width = 80
	m.height = 24
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	mm := tm.(*model)
	if mm.input.Width() != 140 {
		t.Fatalf("expected input width 140, got %d", mm.input.Width())
	}
}

func TestCtrlLClearsScrollback(t *testing.T) {
	fc := &fakeControl{model: "test"}
	m := newModel(fc, testCfg())
	m.width = 80
	m.height = 24
	m.sb.appendUser("hello world")
	m.rebuild()
	v := m.View()
	if !strings.Contains(v, "hello world") {
		t.Fatal("expected scrollback content before Ctrl+L")
	}
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	mm := tm.(*model)
	v = mm.View()
	if strings.Contains(v, "hello world") {
		t.Fatal("scrollback should be cleared after Ctrl+L")
	}
}
