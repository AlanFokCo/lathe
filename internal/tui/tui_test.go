package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/event"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeRunner provides Run (the streaming channel); fakeControl embeds it and
// adds SetModel/ListModels/ModelName so it satisfies EngineControl.
type fakeRunner struct {
	events []event.Event
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) <-chan event.Event {
	ch := make(chan event.Event, len(f.events))
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

func TestModelRendersStreamingTextTurn(t *testing.T) {
	runner := &fakeControl{model: "gpt-4o", fakeRunner: fakeRunner{events: []event.Event{
		event.TextDelta{Delta: "Hel"},
		event.TextDelta{Delta: "lo"},
		event.Usage{InputTokens: 1, OutputTokens: 2, Model: "gpt-4o"},
		event.ReplyEnd{Reason: "end_turn"},
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
	m.handleEvent(event.Usage{InputTokens: 10, OutputTokens: 5, Model: "gpt-4o"})
	m.handleEvent(event.Usage{InputTokens: 3, OutputTokens: 2, Model: "gpt-4o"})
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
	m.handleEvent(event.Compacted{Before: 1000, After: 100})
	got := m.View()
	if !strings.Contains(got, "1000") || !strings.Contains(got, "100") {
		t.Fatalf("scrollback missing compacted tokens:\n%s", got)
	}
}

func TestModelRequireApprovalShowsModal(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(event.RequireApproval{ID: "t1", ToolName: "Bash", Input: `{"command":"ls"}`})
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
		m.handleEvent(event.RequireApproval{ID: "t1", ToolName: "Bash", Input: `{}`})
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
	m.handleEvent(event.RequireApproval{ID: "t1", ToolName: "Bash", Input: `{}`})
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if len(ctrl.approvalCalls) != 1 || ctrl.approvalCalls[0] != "deny" {
		t.Fatalf("ESC: got %v want deny", ctrl.approvalCalls)
	}
}

func TestStatusLineFallback(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.handleEvent(event.Usage{InputTokens: 7, OutputTokens: 3, Model: "gpt-4o"})
	got := m.View()
	if !strings.Contains(got, "model=gpt-4o") || !strings.Contains(got, "in=7 out=3") {
		t.Fatalf("fallback status missing:\n%s", got)
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
		fakeRunner: fakeRunner{events: []event.Event{
			event.Usage{InputTokens: 5, OutputTokens: 1, Model: "gpt-4o"},
			event.ReplyEnd{Reason: "end_turn"},
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

func TestActivityLineThinking(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(event.TurnStep{Iter: 2, MaxIters: 50})
	got := m.View()
	if !strings.Contains(got, "thinking") || !strings.Contains(got, "step 2/50") {
		t.Fatalf("activity line missing thinking/step:\n%s", got)
	}
}

func TestActivityLineRunning(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.handleEvent(event.TurnStep{Iter: 3, MaxIters: 50})
	m.handleEvent(event.ToolCallStart{ID: "t1", Name: "Bash", Input: `{}`})
	got := m.View()
	if !strings.Contains(got, "running Bash") || !strings.Contains(got, "step 3/50") {
		t.Fatalf("activity line missing running tool:\n%s", got)
	}
}

func TestActivityLineHiddenWhenIdle(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg()) // stateIdle
	m.handleEvent(event.TurnStep{Iter: 1, MaxIters: 50})
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
