// Package tui is lathe's interactive terminal UI: a bubbletea program that
// consumes the agent.Engine event stream. M3a adds /model /config slash +
// a cost status line + EngineControl interface.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	asevent "github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/statusline"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// EngineControl is the subset of *agent.Engine the TUI depends on. M6a Commit
// B: Run returns a channel of agentscope events (no translator/internal/event).
type EngineControl interface {
	Run(ctx context.Context, prompt string) <-chan asevent.Event
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
	eventCh        <-chan asevent.Event
	cumIn          int
	cumOut         int
	pendingTool    string
	statusLineText string
	slGen          int
	spinner        spinner.Model
	phase          activityPhase
	curTool        string
	width          int
	height         int
	viewport       viewport.Model
	selectedTool   int // M5d: index into sb.blocks of the highlighted tool block, -1 none
	cwd            string
	dirty          bool // M6a: TextDelta marked scrollback dirty; drained by formatTick
	rebuildN       int  // M6a: rebuild() call counter (test observability)
}

func newModel(engine EngineControl, cfg *config.Config) *model {
	ta := textarea.New()
	ta.Prompt = ""             // M5c-2: drop the default "┃ " vertical line below the input
	ta.ShowLineNumbers = false // M5c-2: drop the line-number gutter
	ta.SetWidth(80)
	sp := spinner.New()
	vp := viewport.New(80, 24)
	cwd, _, _, _ := engine.StatusInfo()
	return &model{engine: engine, cfg: cfg, input: ta, state: stateIdle, spinner: sp, viewport: vp, selectedTool: -1, cwd: cwd}
}

// wrapWidth returns the terminal width for wrapping/glamour, defaulting to 80
// before the first tea.WindowSizeMsg arrives.
func (m *model) wrapWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.input.Focus(), m.scheduleStatusLine()) }

// submit starts a turn: appends the user prompt, runs the engine, begins pumping.
func (m *model) submit(prompt string) tea.Cmd {
	m.sbAppendUser(prompt)
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx, m.cancel = ctx, cancel
	m.state = stateRunning
	m.eventCh = m.engine.Run(ctx, prompt)
	return tea.Batch(waitForEvent(m.eventCh), m.spinner.Tick, scheduleFormatTick())
}

// rebuild re-renders the scrollback into the viewport (M5d). If the user is
// currently pinned to the bottom, re-snap; otherwise leave their scroll
// position alone (claude-code-style "don't yank back while reading history").
func (m *model) rebuild() {
	m.rebuildN++ // M6a: observability for the redraw-throttle test
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.sb.build(m.wrapWidth(), m.selectedTool))
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// toolBlockIndices returns sb block indices of finished tool blocks (M5d).
func (m *model) toolBlockIndices() []int {
	var idx []int
	for i, b := range m.sb.blocks {
		if b.kind == kindTool && b.done {
			idx = append(idx, i)
		}
	}
	return idx
}

// selectTool moves selection among finished tool blocks by dir (+1/-1),
// wrapping. Defaults to the last block for ] and first for [. M5d.
func (m *model) selectTool(dir int) {
	idx := m.toolBlockIndices()
	if len(idx) == 0 {
		m.selectedTool = -1
		return
	}
	pos := -1
	for i, bi := range idx {
		if bi == m.selectedTool {
			pos = i
			break
		}
	}
	if pos < 0 {
		if dir > 0 {
			pos = len(idx) - 1
		} else {
			pos = 0
		}
	} else {
		pos += dir
		if pos < 0 {
			pos = len(idx) - 1
		} else if pos >= len(idx) {
			pos = 0
		}
	}
	m.selectedTool = idx[pos]
	m.scrollSelectedIntoView()
}

// toggleExpand flips the expanded flag on the selected finished tool block.
// Returns false if nothing is selected to expand. M5d.
func (m *model) toggleExpand() bool {
	if m.selectedTool < 0 || m.selectedTool >= len(m.sb.blocks) {
		return false
	}
	b := &m.sb.blocks[m.selectedTool]
	if b.kind != kindTool || !b.done {
		return false
	}
	b.expanded = !b.expanded
	return true
}

// scrollSelectedIntoView brings the selected block into the visible viewport
// window using its startLine. M5d.
func (m *model) scrollSelectedIntoView() {
	if m.selectedTool < 0 || m.selectedTool >= len(m.sb.blocks) {
		return
	}
	start := m.sb.blocks[m.selectedTool].startLine
	vp := &m.viewport
	if start < vp.YOffset {
		vp.SetYOffset(start)
	} else if start >= vp.YOffset+vp.Height {
		vp.SetYOffset(start - vp.Height + 1)
	}
}

// sbAppendUser appends a user block and immediately rebuilds, for callers
// outside handleEvent (submit, slash commands) where no end-of-handler
// rebuild runs. M5d.
func (m *model) sbAppendUser(s string) {
	m.sb.appendUser(s)
	m.rebuild()
}

// eventMsg wraps one engine event for the bubbletea Update loop.
type eventMsg struct{ ev asevent.Event }

// streamEndMsg is sent when the engine event channel closes.
type streamEndMsg struct{}

// formatTickMsg drives the throttled (120ms) scrollback rebuild while a turn
// is running (M5d). Decouples token-stream rate from glamour rate so the
// spinner tick never touches glamour.
type formatTickMsg struct{}

func scheduleFormatTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return formatTickMsg{} })
}

func waitForEvent(ch <-chan asevent.Event) tea.Cmd {
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
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3 // 3 pinned lines: activity + status + input (M5d)
		m.rebuild()
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
		// M5d: viewport scroll + tool block selection/expand. These run for any
		// non-approval state. Expand/selection keys only act when input is empty
		// so typing into the textarea is unaffected (except 'e', which is gated
		// on a selected tool existing).
		switch {
		case msg.Type == tea.KeyPgUp:
			m.viewport.PageUp()
			return m, nil
		case msg.Type == tea.KeyPgDown:
			m.viewport.PageDown()
			return m, nil
		case m.input.Value() == "" && msg.Type == tea.KeyEnter:
			if m.toggleExpand() {
				m.rebuild()
				return m, nil
			}
		case m.input.Value() == "" && msg.Type == tea.KeyRunes && len(msg.Runes) == 1:
			switch msg.Runes[0] {
			case ']', '[':
				dir := 1
				if msg.Runes[0] == '[' {
					dir = -1
				}
				m.selectTool(dir)
				m.rebuild()
				return m, nil
			case 'e':
				if m.selectedTool >= 0 && m.toggleExpand() {
					m.rebuild()
					return m, nil
				}
			}
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
		if _, end := msg.ev.(asevent.ReplyEndEvent); end {
			m.state = stateIdle
			m.cancel = nil
			return m, m.scheduleStatusLine()
		}
		return m, waitForEvent(m.eventCh)
	case streamEndMsg:
		m.sb.finishAssistant()
		m.state = stateIdle
		m.cancel = nil
		m.phase = phaseIdle
		return m, m.scheduleStatusLine()
	case statusLineMsg:
		if msg.gen == m.slGen {
			m.statusLineText = msg.text
		}
		return m, nil
	case formatTickMsg:
		if m.state == stateRunning {
			if m.dirty { // M6a: only rebuild when streaming actually changed content
				m.rebuild()
				m.dirty = false
			}
			return m, scheduleFormatTick()
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

// handleEvent consumes an agentscope block-lifecycle event (M6a Commit B).
// TextBlockDelta defers its rebuild to the 120ms formatTick (returns early);
// all other events rebuild immediately.
func (m *model) handleEvent(ev asevent.Event) {
	switch e := ev.(type) {
	case asevent.TextBlockDeltaEvent:
		m.sb.appendAssistantText(e.Delta)
		m.dirty = true // M6a: defer the O(blocks) rebuild to the 120ms formatTick
		return
	case asevent.ModelCallStartEvent:
		m.phase = phaseThinking
	case asevent.ModelCallEndEvent:
		m.cumIn += e.InputTokens
		m.cumOut += e.OutputTokens
	case asevent.ToolCallStartEvent:
		// agentscope's ToolCallStartEvent carries only ID+Name (no Input).
		m.sb.appendTool(e.ToolCallID, e.ToolCallName, "")
		m.phase = phaseRunning
		m.curTool = e.ToolCallName
	case asevent.ToolResultTextDeltaEvent:
		m.sb.appendToolResultDelta(e.ToolCallID, e.Delta)
	case asevent.ToolResultEndEvent:
		m.sb.finishTool(e.ToolCallID, string(e.State))
		m.phase = phaseThinking
	case asevent.RequireUserConfirmEvent:
		if len(e.ToolCalls) > 0 {
			m.pendingTool = e.ToolCalls[0].Name
		}
		m.state = stateAwaitingApproval
	case asevent.CustomEvent:
		switch e.Name {
		case "compacted":
			before, hasBefore := e.Value["before"]
			after, hasAfter := e.Value["after"]
			if hasBefore && hasAfter {
				m.sb.appendUser(fmt.Sprintf("context compressed: %v→%v tokens", before, after))
			} else {
				m.sb.appendUser("context compressed")
			}
		case "error":
			if errMsg, ok := e.Value["error"].(string); ok {
				m.sb.appendError(fmt.Errorf("%s", errMsg))
			}
		}
	case asevent.ReplyEndEvent:
		m.sb.finishAssistant()
		m.phase = phaseIdle
	}
	m.rebuild() // M5d: refresh viewport content after every event
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
	parts := []string{m.engine.ModelName()}
	if m.cwd != "" {
		parts = append(parts, m.cwd)
	}
	parts = append(parts, fmt.Sprintf("in=%d out=%d", m.cumIn, m.cumOut))
	return strings.Join(parts, " · ")
}

// activityLine returns the live progress line shown while a turn runs:
// "⠋ thinking" or "⠋ running <tool>". Empty when idle. (M6a Commit B: dropped
// the step N/max display — agentscope has no TurnStep/iter event.)
func (m *model) activityLine() string {
	if m.state != stateRunning {
		return ""
	}
	label := "thinking"
	if m.phase == phaseRunning {
		label = "running " + m.curTool
	}
	return m.spinner.View() + " " + label
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
		m.rebuild()
		return nil, true
	case "help":
		m.sbAppendUser("/help: commands: /help /clear /quit /compact /model [name] /config")
		return nil, true
	case "compact":
		msg, err := m.engine.CompressNow(context.Background())
		if err != nil {
			m.sbAppendUser("/compact: " + err.Error())
		} else {
			m.sbAppendUser("/compact: " + msg)
		}
		return nil, true
	case "model":
		return m.handleModel(rest), true
	case "config":
		m.sbAppendUser(configString(m.cfg))
		return nil, true
	default:
		m.sbAppendUser("unknown command: /" + cmd + " " + rest)
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
		m.sbAppendUser(b.String())
		return nil
	}
	if err := m.engine.SetModel(rest); err != nil {
		m.sbAppendUser("/model " + rest + ": " + err.Error())
		return nil
	}
	m.sbAppendUser("/model: switched to " + rest)
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
	bottom := m.statusLine() + "\n" + promptStyle.Render("> ") + m.input.View()
	if m.state == stateAwaitingApproval {
		return m.viewport.View() + "\n" +
			fmt.Sprintf("Approve %s? [y]es / [n]o / [a]lways (ESC=deny)", m.pendingTool) + "\n" + bottom
	}
	// M5d: activity line is always present (empty when idle) so the pinned
	// area is a stable 3 lines — no 1-line jitter between running/idle.
	return m.viewport.View() + "\n" + m.activityLine() + "\n" + bottom
}
