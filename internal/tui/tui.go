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
	"github.com/alanfokco/lathe/internal/tui/theme"
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
	dirty          bool      // M6a: TextDelta marked scrollback dirty; drained by formatTick
	rebuildN       int       // M6a: rebuild() call counter (test observability)
	turnStart      time.Time // M6c: current turn start, for elapsed + tok/s
	lastIn         int       // M6c: last ModelCallEnd InputTokens ≈ context used
	ctxSize        int       // M6c: model context-window size (from StatusInfo)
}

func newModel(engine EngineControl, cfg *config.Config) *model {
	applyTheme(theme.Get(cfg.Theme)) // M6b: resolve the TUI theme
	ta := textarea.New()
	ta.Prompt = ""             // M5c-2: drop the default "┃ " vertical line below the input
	ta.ShowLineNumbers = false // M5c-2: drop the line-number gutter
	ta.SetWidth(80)
	sp := spinner.New()
	vp := viewport.New(80, 24)
	cwd, _, _, ctxSize := engine.StatusInfo()
	return &model{engine: engine, cfg: cfg, input: ta, state: stateIdle, spinner: sp, viewport: vp, selectedTool: -1, cwd: cwd, ctxSize: ctxSize}
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
	m.turnStart = time.Now() // M6c: for elapsed + tok/s in the activity line
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
		m.lastIn = e.InputTokens // M6c: last request's input ≈ current context usage
	case asevent.ToolCallStartEvent:
		// M6b: ToolCallStartEvent now carries the tool's JSON input (enriched
		// upstream) — used for the header arg + lexer selection.
		m.sb.appendTool(e.ToolCallID, e.ToolCallName, e.ToolCallInput)
		m.phase = phaseRunning
		m.curTool = e.ToolCallName
	case asevent.ToolResultTextDeltaEvent:
		m.sb.appendToolResultDelta(e.ToolCallID, e.Delta)
	case asevent.ToolResultEndEvent:
		// M6b: tool-result Metadata (enriched upstream) carries the Edit/Write
		// diff, rendered by the colored-diff renderer.
		m.sb.finishTool(e.ToolCallID, string(e.State), diffFromMeta(e.Metadata))
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

// diffFromMeta extracts a unified diff from a tool-result event's metadata
// (Edit/Write set Metadata["diff"] upstream). Empty when absent. M6b.
func diffFromMeta(m map[string]any) string {
	if m == nil {
		return ""
	}
	if d, ok := m["diff"].(string); ok {
		return d
	}
	return ""
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
	if cb := contextBar(m.lastIn, m.ctxSize); cb != "" { // M6c: context-window usage
		parts = append(parts, cb)
	}
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
	out := m.spinner.View() + " " + label
	if !m.turnStart.IsZero() { // M6c: elapsed + throughput
		d := time.Since(m.turnStart)
		out += " · " + formatElapsed(d)
		if tps := tokPerSec(m.cumOut, d); tps > 0 {
			out += fmt.Sprintf(" · %d tok/s", tps)
		}
	}
	return out
}

// maybeSlash dispatches a "/cmd rest" input via the command registry (M6c).
// Returns (cmd, true) if the input was a slash command.
func (m *model) maybeSlash(input string) (tea.Cmd, bool) {
	name, rest, ok := parseSlash(input)
	if !ok {
		return nil, false
	}
	c, found := lookupCommand(name)
	if !found {
		m.sbAppendUser("unknown command: /" + name + " " + rest)
		return nil, true
	}
	return c.run(m, rest), true
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

// handleTheme shows or switches the TUI theme live (M6c). A switch re-applies
// the palette and forces assistant blocks to re-render at the new glamour style.
func (m *model) handleTheme(rest string) tea.Cmd {
	if rest == "" {
		var b strings.Builder
		b.WriteString("/theme: current=" + curTheme.Name + "\n")
		for _, name := range []string{"lathe-dark", "light"} {
			mark := "  "
			if name == curTheme.Name {
				mark = "* "
			}
			b.WriteString(mark + name + "\n")
		}
		m.sbAppendUser(strings.TrimRight(b.String(), "\n"))
		return nil
	}
	applyTheme(theme.Get(rest))
	m.sb.invalidateRenders()
	m.rebuild()
	m.sbAppendUser("/theme: switched to " + curTheme.Name)
	return nil
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
