// Package tui is lathe's interactive terminal UI: a bubbletea program that
// consumes the agent.Engine event stream. M3a adds /model /config slash +
// a cost status line + EngineControl interface.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/event"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// EngineControl is the subset of *agent.Engine the TUI depends on.
type EngineControl interface {
	Run(ctx context.Context, prompt string) <-chan event.Event
	SetModel(name string) error
	ListModels() []string
	ModelName() string
	CompressNow(ctx context.Context) (string, error)
}

type modelState int

const (
	stateIdle modelState = iota
	stateRunning
)

type model struct {
	engine  EngineControl
	cfg     *config.Config
	input   textarea.Model
	sb      scrollback
	state   modelState
	ctx     context.Context
	cancel  context.CancelFunc
	eventCh <-chan event.Event
	cumIn   int
	cumOut  int
}

func newModel(engine EngineControl, cfg *config.Config) *model {
	ta := textarea.New()
	return &model{engine: engine, cfg: cfg, input: ta, state: stateIdle}
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
		m.cumIn += e.InputTokens
		m.cumOut += e.OutputTokens
	case event.ErrorEvent:
		m.sb.appendError(e.Err)
	case event.Compacted:
		m.sb.appendUser(fmt.Sprintf("context compressed: %d→%d tokens", e.Before, e.After))
	}
}

// maybeSlash handles /help /clear /quit /model /config. Returns (cmd, true) if
// input was a slash command.
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
		m.sb.appendUser("/help: commands: /help /clear /quit /compact /model [name] /config")
		return nil, true
	case "compact":
		msg, err := m.engine.CompressNow(context.Background())
		if err != nil {
			m.sb.appendUser("/compact: " + err.Error())
		} else {
			m.sb.appendUser("/compact: " + msg)
		}
		return nil, true
	case "model":
		m.handleModel(rest)
		return nil, true
	case "config":
		m.sb.appendUser(configString(m.cfg))
		return nil, true
	default:
		m.sb.appendUser("unknown command: /" + cmd + " " + rest)
		return nil, true
	}
}

func (m *model) handleModel(rest string) {
	if rest == "" {
		cur := m.engine.ModelName()
		var b strings.Builder
		b.WriteString("/model: current=" + cur + "\n")
		for _, name := range m.engine.ListModels() {
			mark := "  "
			if name == cur {
				mark = "* "
			}
			b.WriteString(mark + name + "\n")
		}
		m.sb.appendUser(b.String())
		return
	}
	if err := m.engine.SetModel(rest); err != nil {
		m.sb.appendUser("/model " + rest + ": " + err.Error())
		return
	}
	m.sb.appendUser("/model: switched to " + rest)
}

func configString(cfg *config.Config) string {
	return fmt.Sprintf("provider=%s\nmodel=%s\npermission=%s\nmax-iters=%d\nbase-url=%s\napi-key=%s",
		cfg.Provider, cfg.Model, cfg.Permission, cfg.MaxIters, cfg.BaseURL, redactKey(cfg.APIKey))
}

func redactKey(k string) string {
	if len(k) <= 4 {
		return strings.Repeat("*", len(k))
	}
	return k[:4] + "…"
}

func (m *model) View() string {
	status := fmt.Sprintf("model=%s | perm=%s | tokens in=%d out=%d",
		m.engine.ModelName(), m.cfg.Permission, m.cumIn, m.cumOut)
	return m.sb.render(80) + "\n" + status + "\n" + m.input.View()
}
