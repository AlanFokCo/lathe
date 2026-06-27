package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/event"
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
	model  string
	models []string
	sets   []string
}

func (f *fakeControl) SetModel(name string) error { f.sets = append(f.sets, name); f.model = name; return nil }
func (f *fakeControl) ListModels() []string       { return f.models }
func (f *fakeControl) ModelName() string          { return f.model }

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
func pumpModel(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		_, c = m.Update(msg)
		cmd = c
	}
}
