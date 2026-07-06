// Package tui is lathe's interactive terminal UI: a bubbletea program that
// consumes the agent.Engine event stream. M3a adds /model /config slash +
// a cost status line + EngineControl interface.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/statusline"
	"github.com/alanfokco/lathe/internal/subagent"
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
	MCPServers() []mcpconfig.ServerInfo       // M6c-5: /mcp
	ListSessions() []session.Summary          // M6c-5: /resume
	ToolNames() []string                      // M6f: /tools
	SetThinking(enable bool, budget int)      // M7a: /thinking on|off|budget=N
	Thinking() (enable bool, budget int)      // M7a: report current
	SetEffort(level string)                   // M7b: /effort low|medium|high|off
	Effort() string                           // M7b: report current
	EnterPlanMode()                           // M7g: /plan on
	ExitPlanMode()                            // M7g: /plan off
	ApprovePlan()                             // M10b: /plan approve → exit plan + accept_edits
	IsPlanMode() bool                         // M7g: status line + slash report
	PermissionMode() string                   // M10c: status line + Shift+Tab
	SetPermissionMode(mode string)            // M10c: Shift+Tab cycling
	Subagents() []subagent.SubagentInfo       // M7e: /agents
	Jailed() bool                             // M7f: /sandbox
	SandboxMode() string                      // M7f: /sandbox
	SkillsList() []skill.Skill                // M8c: /skills
	HooksList() map[string][]settings.Matcher // M8c: /hooks
	AgentscopeVersion() string                // M9a: /doctor
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
	dirty          bool                        // M6a: TextDelta marked scrollback dirty; drained by formatTick
	rebuildN       int                         // M6a: rebuild() call counter (test observability)
	turnStart      time.Time                   // M6c: current turn start, for elapsed + tok/s
	lastIn         int                         // M6c: last ModelCallEnd InputTokens ≈ context used
	ctxSize        int                         // M6c: model context-window size (from StatusInfo)
	paletteCursor  int                         // M6c-3: selected index in the slash command palette
	cumCacheR      int                         // M6c-4: cumulative cache-read tokens
	cumCacheW      int                         // M6c-4: cumulative cache-creation tokens
	todos          []tool.Task                 // M6h: live TodoWrite tracker
	todoBufs       map[string]*strings.Builder // M6h: id → task_* payload accumulator
	hist           *history                    // M8b: ↑/↓ input history recall
	lastCtrlC      time.Time                   // M8b: Ctrl+C double-confirm timestamp
	filepick       *filePicker                 // M9c: @file autocomplete backend
	pickerCursor   int                         // M9c: selected file-picker match index
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
	m := &model{engine: engine, cfg: cfg, input: ta, state: stateIdle, spinner: sp, viewport: vp, selectedTool: -1, cwd: cwd, ctxSize: ctxSize}
	m.hist = newHistory(defaultHistoryPath()) // M8b: persistent input history
	m.filepick = newFilePicker(cwd)           // M9c: @file autocomplete
	return m
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

// submit starts a turn: appends the user prompt (as typed, for the scrollback)
// and dispatches the expanded prompt (M7d.1 @file expansion applied) to the
// engine. Preserving the raw typed version in scrollback keeps the display
// clean; the model still sees the inlined file contents.
func (m *model) submit(prompt string) tea.Cmd {
	m.sbAppendUser(prompt)
	expanded := expandAtFiles(prompt, m.cwd)
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx, m.cancel = ctx, cancel
	m.state = stateRunning
	m.turnStart = time.Now() // M6c: for elapsed + tok/s in the activity line
	m.eventCh = m.engine.Run(ctx, expanded)
	return tea.Batch(waitForEvent(m.eventCh), m.spinner.Tick, scheduleFormatTick())
}

// rebuild re-renders the scrollback into the viewport (M5d). If the user is
// currently pinned to the bottom, re-snap; otherwise leave their scroll
// position alone (claude-code-style "don't yank back while reading history").
// M6h: also re-applies layout so a growing/shrinking todo pane resizes the
// viewport instead of pushing the input off-screen.
func (m *model) rebuild() {
	m.rebuildN++ // M6a: observability for the redraw-throttle test
	m.applyLayout()
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.sb.build(m.wrapWidth(), m.selectedTool))
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// applyLayout resizes the viewport based on current terminal dims minus the
// three pinned lines (activity + status + input) minus the todo pane height.
// M6h. No-op before the first WindowSizeMsg (width/height=0).
func (m *model) applyLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	h := m.height - 3 - m.todoPaneHeight()
	if h < 1 {
		h = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = h
}

// todoPaneHeight returns the number of lines todoPane() will produce (M6h).
func (m *model) todoPaneHeight() int {
	if len(m.todos) == 0 {
		return 0
	}
	const cap = 5
	shown := len(m.todos)
	overflow := 0
	if shown > cap {
		overflow = shown - cap
		shown = cap
	}
	lines := 1 + shown // "todos:" header + shown entries
	if overflow > 0 {
		lines++ // "… N more"
	}
	return lines
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
		// M6h: rebuild() invokes applyLayout() which now factors in the todo pane.
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
		// M9c: @file picker — when idle and the input has a trailing @query
		// (no whitespace after @), arrows navigate file matches, Tab/Enter
		// inserts the selected file path, Esc dismisses. Takes priority over
		// the slash palette (which only triggers on input starting with "/").
		if m.state == stateIdle && m.filepick != nil {
			if q, prefix, ok := extractAtQuery(m.input.Value()); ok {
				if items := m.filepick.Match(q); len(items) > 0 {
					switch msg.Type {
					case tea.KeyUp:
						m.pickerCursor = (m.pickerCursor - 1 + len(items)) % len(items)
						return m, nil
					case tea.KeyDown:
						m.pickerCursor = (m.pickerCursor + 1) % len(items)
						return m, nil
					case tea.KeyTab, tea.KeyEnter:
						if m.pickerCursor >= len(items) {
							m.pickerCursor = 0
						}
						m.input.SetValue(prefix + items[m.pickerCursor] + " ")
						m.pickerCursor = 0
						return m, nil
					case tea.KeyEscape:
						m.pickerCursor = 0
						return m, nil
					}
				}
			}
		}
		// M6c-3: slash command palette — when idle and the input is a bare
		// "/prefix" (no space), arrows navigate matches, Tab completes, Enter runs
		// the selected command, Esc dismisses. Other keys fall through to typing.
		if m.state == stateIdle {
			if items := paletteItems(m.input.Value()); len(items) > 0 {
				switch msg.Type {
				case tea.KeyUp:
					m.paletteCursor = (m.paletteCursor - 1 + len(items)) % len(items)
					return m, nil
				case tea.KeyDown:
					m.paletteCursor = (m.paletteCursor + 1) % len(items)
					return m, nil
				case tea.KeyTab:
					if m.paletteCursor >= len(items) {
						m.paletteCursor = 0
					}
					m.input.SetValue("/" + items[m.paletteCursor].name + " ")
					m.paletteCursor = 0
					return m, nil
				case tea.KeyEscape:
					m.input.Reset()
					m.paletteCursor = 0
					return m, nil
				case tea.KeyEnter:
					if m.paletteCursor >= len(items) {
						m.paletteCursor = 0
					}
					name := items[m.paletteCursor].name
					m.input.Reset()
					m.paletteCursor = 0
					cmd, _ := m.maybeSlash("/" + name)
					return m, cmd
				}
			}
		}
		// M8b: input history recall when input is empty OR already browsing.
		// The Browsing() guard lets successive ↑/↓ keep walking through
		// history after Prev populated the input (which would otherwise fail
		// the empty check on the next keystroke).
		if m.state == stateIdle && m.hist != nil && (m.input.Value() == "" || m.hist.Browsing()) {
			switch msg.Type {
			case tea.KeyUp:
				if v, ok := m.hist.Prev(); ok || v != "" {
					m.input.SetValue(v)
					return m, nil
				}
			case tea.KeyDown:
				if v, ok := m.hist.Next(); ok {
					m.input.SetValue(v)
					return m, nil
				}
			}
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
			// M8b: Ctrl+C is contextual. While a turn is running it cancels
			// the turn (same as Esc). While idle it needs a second press
			// within 2s to actually quit, so users cannot lose a session by
			// muscle-memory (bash-style).
			if m.state == stateRunning {
				if m.cancel != nil {
					m.cancel()
				}
				return m, nil
			}
			if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) < 2*time.Second {
				return m, tea.Quit
			}
			m.lastCtrlC = time.Now()
			return m, nil
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
			if m.hist != nil {
				m.hist.Append(text) // M8b: persist to history before dispatch
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
		m.paletteCursor = 0 // M6c-3: typing re-filters, reset palette selection
		if m.hist != nil {
			m.hist.ResetBrowse() // M8b: any typing/edit forgets the recall position
		}
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
	case asevent.ThinkingBlockStartEvent:
		// M7a: extended thinking. Start a new thinking block so subsequent
		// deltas append to it, and reflect the phase for the activity line.
		m.sb.appendThinkingDelta("")
		m.phase = phaseThinking
	case asevent.ThinkingBlockDeltaEvent:
		m.sb.appendThinkingDelta(e.Delta)
		m.dirty = true // batch with the formatTick like text deltas
	case asevent.ThinkingBlockEndEvent:
		m.sb.finishThinking()
	case asevent.ModelCallEndEvent:
		m.cumIn += e.InputTokens
		m.cumOut += e.OutputTokens
		m.lastIn = e.InputTokens // M6c: last request's input ≈ current context usage
		m.cumCacheR += e.CacheReadTokens
		m.cumCacheW += e.CacheCreationTokens
	case asevent.ToolCallStartEvent:
		// M6b: ToolCallStartEvent now carries the tool's JSON input (enriched
		// upstream) — used for the header arg + lexer selection.
		m.sb.appendTool(e.ToolCallID, e.ToolCallName, e.ToolCallInput)
		m.phase = phaseRunning
		m.curTool = e.ToolCallName
		// M6h: remember the tool name by call id so ToolResultEnd can decide
		// whether to feed the accumulated payload into the todo tracker.
		if strings.HasPrefix(e.ToolCallName, "task_") {
			if m.todoBufs == nil {
				m.todoBufs = map[string]*strings.Builder{}
			}
			m.todoBufs[e.ToolCallID] = &strings.Builder{}
		}
	case asevent.ToolResultTextDeltaEvent:
		m.sb.appendToolResultDelta(e.ToolCallID, e.Delta)
		// M6h: mirror the delta into the todo buffer (if this call was a task_*).
		if buf, ok := m.todoBufs[e.ToolCallID]; ok {
			buf.WriteString(e.Delta)
		}
	case asevent.ToolResultEndEvent:
		// M6b: tool-result Metadata (enriched upstream) carries the Edit/Write
		// diff, rendered by the colored-diff renderer.
		m.sb.finishTool(e.ToolCallID, string(e.State), diffFromMeta(e.Metadata))
		m.phase = phaseThinking
		// M6h: on task_* completion, parse the accumulated JSON payload and
		// merge into the todo tracker. Ignore parse errors — the tool result
		// is user-visible in scrollback, so a silent skip is fine.
		if buf, ok := m.todoBufs[e.ToolCallID]; ok {
			m.mergeTodoPayload(buf.String())
			delete(m.todoBufs, e.ToolCallID)
		}
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

// mergeTodoPayload parses a task_* tool result JSON payload (a single Task, as
// task_create/get/update return, OR an array of Tasks, as task_list returns)
// and merges into m.todos by id, preserving insertion order for new ids. M6h.
func (m *model) mergeTodoPayload(payload string) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return
	}
	if strings.HasPrefix(payload, "[") {
		var arr []tool.Task
		if err := json.Unmarshal([]byte(payload), &arr); err != nil {
			return
		}
		for _, t := range arr {
			m.upsertTodo(t)
		}
		return
	}
	var one tool.Task
	if err := json.Unmarshal([]byte(payload), &one); err != nil {
		return
	}
	m.upsertTodo(one)
}

// upsertTodo replaces an existing todo by id in place, else appends. M6h.
func (m *model) upsertTodo(t tool.Task) {
	if t.ID == "" {
		return
	}
	for i := range m.todos {
		if m.todos[i].ID == t.ID {
			m.todos[i] = t
			return
		}
	}
	m.todos = append(m.todos, t)
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
	if est := m.dollarEstimate(); est != "" { // M9b: running dollar spend
		// strip the "cost: " prefix in the status line for compactness
		parts = append(parts, strings.TrimPrefix(est, "cost: "))
	}
	if m.engine.IsPlanMode() { // M7g: prominent PLAN marker so read-only mode is impossible to miss
		parts = append(parts, warnStyle.Render("PLAN"))
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
	// M5d: activity line is always present (empty when idle) so the pinned area
	// is a stable 3 lines. M6c-3: when a slash palette is active (idle), it takes
	// that middle line instead (single-line to keep the height stable).
	mid := m.activityLine()
	if m.state == stateIdle {
		// M9c: @file picker takes priority over palette + Ctrl+C hint.
		if q, _, ok := extractAtQuery(m.input.Value()); ok && m.filepick != nil {
			if items := m.filepick.Match(q); len(items) > 0 {
				mid = renderFilePickerPanel(items, m.pickerCursor)
			}
		} else if items := paletteItems(m.input.Value()); len(items) > 0 {
			mid = renderPalette(items, m.paletteCursor)
		} else if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) < 2*time.Second {
			mid = warnStyle.Render("Ctrl+C again to quit") // M8b
		}
	}
	pane := m.todoPane()
	if pane != "" {
		return pane + "\n" + m.viewport.View() + "\n" + mid + "\n" + bottom
	}
	return m.viewport.View() + "\n" + mid + "\n" + bottom
}

// todoPane renders the pinned TodoWrite checklist above the scrollback (M6h).
// Empty string when the tracker is empty (no extra pinned line). Caps at 5
// visible entries so it does not eat the viewport; "... N more" indicates
// overflow. States render as [ ] / [~] / [x].
func (m *model) todoPane() string {
	if len(m.todos) == 0 {
		return ""
	}
	const cap = 5
	var b strings.Builder
	b.WriteString("todos:")
	shown := len(m.todos)
	if shown > cap {
		shown = cap
	}
	for i := 0; i < shown; i++ {
		b.WriteString("\n  ")
		b.WriteString(todoMark(m.todos[i].State))
		b.WriteByte(' ')
		b.WriteString(m.todos[i].Subject)
	}
	if extra := len(m.todos) - shown; extra > 0 {
		b.WriteString(fmt.Sprintf("\n  … %d more", extra))
	}
	return b.String()
}

func todoMark(state string) string {
	switch state {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	default:
		return "[ ]"
	}
}
