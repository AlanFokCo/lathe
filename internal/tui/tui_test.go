package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/alanfokco/lathe/internal/event"
)

// fakeRunner is an EngineRunner that returns a scripted event channel.
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

func TestModelRendersStreamingTextTurn(t *testing.T) {
	runner := &fakeRunner{events: []event.Event{
		event.TextDelta{Delta: "Hel"},
		event.TextDelta{Delta: "lo"},
		event.Usage{InputTokens: 1, OutputTokens: 2, Model: "gpt-4o"},
		event.ReplyEnd{Reason: "end_turn"},
	}}
	m := newModel(runner)
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
	runner := &fakeRunner{} // empty events → closed channel; no ReplyEnd pumped
	m := newModel(runner)
	m.submit("hi") // sets state=running, ctx+cancel
	if m.state != stateRunning {
		t.Fatalf("state: %v", m.state)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.ctx == nil || m.ctx.Err() == nil {
		t.Fatal("expected ctx canceled after ESC")
	}
}

func TestModelSlashClear(t *testing.T) {
	m := newModel(&fakeRunner{})
	m.sb.appendUser("old line")
	cmd, ok := m.maybeSlash("/clear")
	if !ok || cmd != nil {
		t.Fatalf("maybeSlash(/clear) = (%v,%v)", cmd, ok)
	}
	if got := m.View(); strings.Contains(got, "old line") {
		t.Fatalf("expected scrollback cleared, got %q", got)
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
