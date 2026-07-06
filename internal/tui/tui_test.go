package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/settings"
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
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                      // expand (input empty)
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
	m.input.Focus() // M5d: mimic Init() focusing the textarea so typed runes land
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
