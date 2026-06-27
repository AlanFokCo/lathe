// Package tui is lathe's interactive terminal UI: a bubbletea program that
// consumes the agent.Engine event stream. M2 = pure event-stream consumer.
package tui

import (
	"context"

	"github.com/alanfokco/lathe/internal/event"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// EngineRunner is the subset of *agent.Engine the TUI depends on (Run only).
// Depending on this interface (not the concrete *Engine) keeps the TUI testable
// with a fake runner — no real engine/terminal needed.
type EngineRunner interface {
	Run(ctx context.Context, prompt string) <-chan event.Event
}

type modelState int

const (
	stateIdle modelState = iota
	stateRunning
)

type model struct {
	engine  EngineRunner
	input   textarea.Model
	sb      scrollback
	state   modelState
	ctx     context.Context
	cancel  context.CancelFunc
	eventCh <-chan event.Event
}

func newModel(engine EngineRunner) *model {
	ta := textarea.New()
	return &model{engine: engine, input: ta, state: stateIdle}
}

func (m *model) Init() tea.Cmd { return m.input.Focus() }

// submit starts a turn: appends the user prompt, runs the engine, begins pumping.
func (m *model) submit(prompt string) tea.Cmd {
	m.sb.appendUser(prompt)
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx, m.cancel = ctx, cancel
	m.state = stateRunning
	m.eventCh = m.engine.Run(ctx, prompt)
	return waitForEvent(m.eventCh)
}

// eventMsg wraps one engine event for the bubbletea Update loop.
type eventMsg struct{ ev event.Event }

// streamEndMsg is sent when the engine event channel closes.
type streamEndMsg struct{}

func waitForEvent(ch <-chan event.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamEndMsg{}
		}
		return eventMsg{ev: ev}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// M2: resize-safe no-op; full width/height wiring is a later refinement.
		return m, nil
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyCtrlC:
			return m, tea.Quit
		case msg.Type == tea.KeyEscape && m.state == stateRunning:
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		case msg.Type == tea.KeyEnter && m.state == stateIdle:
			text := m.input.Value()
			if text == "" {
				return m, nil
			}
			if cmd, ok := m.maybeSlash(text); ok {
				m.input.Reset()
				return m, cmd
			}
			m.input.Reset()
			return m, m.submit(text)
		}
		// default: forward to the textarea
		var c tea.Cmd
		m.input, c = m.input.Update(msg)
		return m, c
	case eventMsg:
		m.handleEvent(msg.ev)
		if _, end := msg.ev.(event.ReplyEnd); end {
			m.state = stateIdle
			m.cancel = nil
			return m, nil
		}
		return m, waitForEvent(m.eventCh)
	case streamEndMsg:
		m.state = stateIdle
		m.cancel = nil
		return m, nil
	}
	return m, nil
}

func (m *model) handleEvent(ev event.Event) {
	switch e := ev.(type) {
	case event.TextDelta:
		m.sb.appendAssistantText(e.Delta)
	case event.ToolCallStart:
		m.sb.appendTool(e.ID, e.Name, e.Input)
	case event.ToolResult:
		m.sb.finishTool(e.ID, e.Output, e.State, e.Diff)
	case event.Usage:
		m.sb.appendUsage(e)
	case event.ErrorEvent:
		m.sb.appendError(e.Err)
	}
}

// maybeSlash handles /help /clear /quit. Returns (cmd, true) if input was a
// slash command.
func (m *model) maybeSlash(input string) (tea.Cmd, bool) {
	cmd, rest, ok := parseSlash(input)
	if !ok {
		return nil, false
	}
	switch cmd {
	case "quit":
		return tea.Quit, true
	case "clear":
		m.sb.clear()
		return nil, true
	case "help":
		m.sb.appendUser("/help: commands: /help /clear /quit")
		return nil, true
	default:
		m.sb.appendUser("unknown command: /" + cmd + " " + rest)
		return nil, true
	}
}

func (m *model) View() string {
	return m.sb.render(80) + "\n" + m.input.View()
}
