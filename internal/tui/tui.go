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
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/statusline"
	"github.com/charmbracelet/bubbles/spinner"
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
	SubmitApproval(decision string)
	StatusInfo() (cwd, sessionID, transcriptPath string, contextSize int)
	StatusLineConfig() *settings.StatusLineConfig
}

type modelState int

const (
	stateIdle modelState = iota
	stateRunning
	stateAwaitingApproval
)

type activityPhase int

const (
	phaseIdle activityPhase = iota
	phaseThinking
	phaseRunning
)

type model struct {
	engine         EngineControl
	cfg            *config.Config
	input          textarea.Model
	sb             scrollback
	state          modelState
	ctx            context.Context
	cancel         context.CancelFunc
	eventCh        <-chan event.Event
	cumIn          int
	cumOut         int
	pendingTool    string
	statusLineText string
	slGen          int
	spinner        spinner.Model
	phase          activityPhase
	curTool        string
	step           int
	maxStep        int
}

func newModel(engine EngineControl, cfg *config.Config) *model {
	ta := textarea.New()
	sp := spinner.New()
	return &model{engine: engine, cfg: cfg, input: ta, state: stateIdle, spinner: sp}
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.input.Focus(), m.scheduleStatusLine()) }

// submit starts a turn: appends the user prompt, runs the engine, begins pumping.
func (m *model) submit(prompt string) tea.Cmd {
	m.sb.appendUser(prompt)
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx, m.cancel = ctx, cancel
	m.state = stateRunning
	m.eventCh = m.engine.Run(ctx, prompt)
	return tea.Batch(waitForEvent(m.eventCh), m.spinner.Tick)
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
		if m.state == stateAwaitingApproval {
			switch {
			case msg.Type == tea.KeyEscape:
				m.engine.SubmitApproval("deny")
				m.state = stateRunning
				return m, nil
			case msg.Type == tea.KeyRunes && len(msg.Runes) == 1:
				switch msg.Runes[0] {
				case 'y':
					m.engine.SubmitApproval("allow")
				case 'n':
					m.engine.SubmitApproval("deny")
				case 'a':
					m.engine.SubmitApproval("always")
				default:
					return m, nil
				}
				m.state = stateRunning
				return m, nil
			}
			return m, nil
		}
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
			return m, m.scheduleStatusLine()
		}
		return m, waitForEvent(m.eventCh)
	case streamEndMsg:
		m.state = stateIdle
		m.cancel = nil
		m.phase = phaseIdle
		return m, m.scheduleStatusLine()
	case statusLineMsg:
		if msg.gen == m.slGen {
			m.statusLineText = msg.text
		}
		return m, nil
	case spinner.TickMsg:
		if m.state == stateRunning || m.state == stateAwaitingApproval {
			var c tea.Cmd
			m.spinner, c = m.spinner.Update(msg)
			return m, c
		}
		return m, nil
	}
	return m, nil
}

func (m *model) handleEvent(ev event.Event) {
	switch e := ev.(type) {
	case event.TextDelta:
		m.sb.appendAssistantText(e.Delta)
	case event.TurnStep:
		m.phase = phaseThinking
		m.step = e.Iter
		m.maxStep = e.MaxIters
	case event.ToolCallStart:
		m.sb.appendTool(e.ID, e.Name, e.Input)
		m.phase = phaseRunning
		m.curTool = e.Name
	case event.ToolResult:
		m.sb.finishTool(e.ID, e.Output, e.State, e.Diff)
		m.phase = phaseThinking
	case event.Usage:
		m.sb.appendUsage(e)
		m.cumIn += e.InputTokens
		m.cumOut += e.OutputTokens
	case event.ErrorEvent:
		m.sb.appendError(e.Err)
	case event.Compacted:
		m.sb.appendUser(fmt.Sprintf("context compressed: %d→%d tokens", e.Before, e.After))
	case event.RequireApproval:
		m.pendingTool = e.ToolName
		m.state = stateAwaitingApproval
	case event.ReplyEnd:
		m.phase = phaseIdle
	}
}

// statusLineMsg carries the result of an async statusline command run.
type statusLineMsg struct {
	gen  int
	text string
}

// slConfig returns the active statusline command config, or zero+false when
// no command is configured.
func (m *model) slConfig() (statusline.Config, bool) {
	slc := m.engine.StatusLineConfig()
	if slc == nil || slc.Type != "command" || slc.Command == "" {
		return statusline.Config{}, false
	}
	return statusline.Config{Type: slc.Type, Command: slc.Command, Padding: slc.Padding}, true
}

// scheduleStatusLine snapshots status data and returns a tea.Cmd that runs the
// configured statusline command (if any). Stale runs are ignored via the gen
// guard in Update. Returns nil when no command is configured.
func (m *model) scheduleStatusLine() tea.Cmd {
	m.slGen++
	gen := m.slGen
	cfg, ok := m.slConfig()
	if !ok {
		m.statusLineText = ""
		return nil
	}
	cwd, sid, tp, ctxSize := m.engine.StatusInfo()
	in := statusline.Input{
		SessionID:      sid,
		TranscriptPath: tp,
		Cwd:            cwd,
		Model:          m.engine.ModelName(),
		Version:        config.Version,
		ContextSize:    ctxSize,
		InputTokens:    m.cumIn,
		OutputTokens:   m.cumOut,
	}
	return func() tea.Msg {
		text, _ := statusline.Run(context.Background(), cfg, in)
		return statusLineMsg{gen: gen, text: text}
	}
}

// statusLine returns the status line for View: the command output when set,
// else the hardcoded fallback.
func (m *model) statusLine() string {
	if m.statusLineText != "" {
		if cfg, ok := m.slConfig(); ok && cfg.Padding > 0 {
			return strings.Repeat(" ", cfg.Padding) + m.statusLineText
		}
		return m.statusLineText
	}
	return fmt.Sprintf("model=%s | perm=%s | tokens in=%d out=%d",
		m.engine.ModelName(), m.cfg.Permission, m.cumIn, m.cumOut)
}

// activityLine returns the live progress line shown while a turn runs:
// "⠋ thinking · step N/max" or "⠋ running <tool> · step N/max". Empty when idle.
func (m *model) activityLine() string {
	if m.state != stateRunning {
		return ""
	}
	label := "thinking"
	if m.phase == phaseRunning {
		label = "running " + m.curTool
	}
	out := m.spinner.View() + " " + label
	if m.maxStep > 0 {
		out += " · step " + fmt.Sprintf("%d/%d", m.step, m.maxStep)
	}
	return out
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
		return m.handleModel(rest), true
	case "config":
		m.sb.appendUser(configString(m.cfg))
		return nil, true
	default:
		m.sb.appendUser("unknown command: /" + cmd + " " + rest)
		return nil, true
	}
}

func (m *model) handleModel(rest string) tea.Cmd {
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
		return nil
	}
	if err := m.engine.SetModel(rest); err != nil {
		m.sb.appendUser("/model " + rest + ": " + err.Error())
		return nil
	}
	m.sb.appendUser("/model: switched to " + rest)
	return m.scheduleStatusLine()
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
	scroll := m.sb.render(80)
	if m.state == stateAwaitingApproval {
		return scroll + "\n" + fmt.Sprintf("Approve %s? [y]es / [n]o / [a]lways (ESC=deny)", m.pendingTool) + "\n" + m.input.View()
	}
	if act := m.activityLine(); act != "" {
		scroll += "\n" + act
	}
	return scroll + "\n" + m.statusLine() + "\n" + m.input.View()
}
