package agent

import (
	"context"
	"fmt"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	agentscope "github.com/alanfokco/agentscope-go/v2/pkg/agentscope"
	asagent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/audit"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/mcp"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/replay"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/resilience"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/pricing"
	"github.com/alanfokco/lathe/internal/hooks"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/skills"
	"github.com/alanfokco/lathe/internal/subagent"
	"github.com/alanfokco/lathe/internal/workspace"
	"github.com/sirupsen/logrus"
)

// Engine is lathe's turn engine. M6a: it is a thin product shell over the
// agentscope UnifiedAgent — it holds the agent plus lathe's product-layer
// concerns (session JSONL, mcp/skill discovery, settings hooks, statusline),
// and Run translates the agent's block-lifecycle events into lathe's event
// stream (temporary; Commit B deletes the translator + internal/event).
type Engine struct {
	name            string
	chatModel       model.ChatModel // resilience-wrapped; for CompressNow/StatusInfo
	agent           *asagent.UnifiedAgent
	toolkit         *tool.Toolkit
	permEng         *permission.Engine
	configuredMode  permission.PermissionMode // cfg.Permission; SetInteractive toggles effective mode
	maxIters        int
	state           *asagent.AgentState // shared with the agent (via WithState); holds Context
	sysPrompt       string              // cached; for CompressNow's compression messages
	cfg             *config.Config
	compressCfg     compressConfig
	session         *session.Session
	mcpClients      []mcp.Client
	mcpServers      []mcpconfig.ServerInfo // M6c-5: name + tool count for /mcp
	hookRunner      *hooks.Runner
	workspaceCloser func() error
	cwd             string                    // M5b: cwd snapshot for statusline payload
	settings        *settings.Settings        // M5b: parsed settings (for StatusLineConfig)
	readCache       *tool.ReadCache           // M6a: read-before-write guard, injected via WithReadCache
	taskCtx         *tool.TaskContext         // M6h: TodoWrite state, wired into Run's ctx via tool.WithTaskContext
	thinker         *thinker                  // M7a: extended-thinking option wrapper (live-toggleable)
	efforter        *efforter                 // M7b: reasoning-effort option wrapper (live-toggleable)
	planActive      bool                      // M7g: /plan on flips this + swaps perm mode
	prePlanMode     permission.PermissionMode // M7g: perm mode to restore on ExitPlanMode
	subagents       *subagent.Tracker         // M7e: subagent lifecycle recorder for /agents
	skillsList      []skill.Skill             // M8c: retained for /skills
	recorder        *replay.Middleware         // replay recording middleware
	auditLogger     audit.Logger               // structured audit logger
	auditPath       string                     // file path for the audit log
	pendingMu       sync.Mutex
	pending         *pendingApproval // HITL bridge: last RequireUserConfirm
}

// NewEngine assembles an Engine from a resolved config (production path:
// builds a real ChatModel for the configured provider). The system prompt is
// built once at construction (env + tool descriptions + project memory); M6b
// will move it to an OnSystemPrompt middleware.
func NewEngine(ctx context.Context, cfg *config.Config) (*Engine, error) {
	initAgentscope()
	cm, err := buildChatModel(cfg)
	if err != nil {
		return nil, err
	}
	// M7a: wrap the model in a thinker so per-call ThinkingEnable/Budget opts
	// can be toggled live via Engine.SetThinking (used by the /thinking slash).
	thk := newThinker(cm, cfg.Thinking, cfg.ThinkingBudget)
	cm = thk
	// M7b: reasoning effort — layered outside thinker so a Chat gets both
	// options in one pass; likewise live-toggleable via /effort.
	ef := newEfforter(cm, cfg.ReasoningEffort)
	cm = ef
	tk := tool.NewEnhancedToolkit()
	configuredMode := permission.PermissionMode(cfg.Permission)
	permCtx := permission.NewContext(printEffectiveMode(configuredMode))
	permEng := permission.NewEngine(permCtx)

	// M6a: one ReadCache per engine, injected via WithReadCache so the base
	// Write/Edit read-before-write guard activates.
	readCache := tool.NewReadCache(0, 0)

	// M4a: discover skills (user ~/.lathe/skills + project .lathe/skills walk-up).
	cwd := mustCwd()
	skillsList, _ := skills.Discover(cwd)
	if len(skillsList) > 0 {
		tk.AddGroup("skills", skill.NewSkillViewerTool(skillsList))
	}

	// M4b: discover MCP servers from .mcp.json (project + user) and register
	// their tools. Clients are stored for Close() on shutdown.
	mcpClients, mcpGroups, mcpWarnings := mcpconfig.Load(ctx, cwd)
	for _, g := range mcpGroups {
		tk.AddGroup("mcp:"+g.Name, g.Tools...)
	}
	for _, w := range mcpWarnings {
		fmt.Fprintln(os.Stderr, "mcp:", w)
	}
	mcpServers := mcpconfig.SummarizeGroups(mcpGroups) // M6c-5: snapshot for /mcp

	// M4c: load settings + build hook runner (settings.json hooks).
	settingsCfg, serr := settings.Load(cwd)
	if serr != nil {
		fmt.Fprintln(os.Stderr, "settings:", serr)
		settingsCfg = &settings.Settings{Hooks: map[string][]settings.Matcher{}}
	}
	hookRunner := hooks.NewRunner(settingsCfg.Hooks, cwd, "")

	// M4e: optional sandbox workspace (default: host builtins). A setup
	// failure fails loudly (no silent fallback to host execution).
	var subToolkit *tool.Toolkit
	var workspaceCloser func() error
	if cfg.Sandbox != "" {
		ws, closer, werr := workspace.NewWorkspace(ctx, cfg.Sandbox, cwd)
		if werr != nil {
			return nil, fmt.Errorf("sandbox: %w", werr)
		}
		tk = workspace.WorkspaceToolkit(ws)
		// Re-add skills + MCP tools that were discovered above so they
		// survive the toolkit swap (sandbox only replaces builtins).
		if len(skillsList) > 0 {
			tk.AddGroup("skills", skill.NewSkillViewerTool(skillsList))
		}
		for _, g := range mcpGroups {
			tk.AddGroup("mcp:"+g.Name, g.Tools...)
		}
		workspaceCloser = closer
		subToolkit = workspace.WorkspaceToolkit(ws)
	} else {
		subToolkit = tool.NewEnhancedToolkit()
	}

	// M4d: Task subagent tool (spawns a nested lathe Engine with a builtins-only
	// toolkit — no Task, so the subagent cannot recurse). In sandbox mode the
	// subagent shares the same workspace (no escape). M7e: shares an engine-
	// level tracker so /agents can list dispatched subagents.
	subTracker := subagent.NewTracker()
	tk.AddGroup("task", NewTaskToolWithTracker(cm, permEng, cfg.MaxIters, subToolkit, subTracker))

	// M6h: TodoWrite toolkit — task_create/get/list/update backed by a per-
	// engine TaskContext (injected into every Run's ctx). TUI consumes the
	// task_* tool result payloads to keep the pinned checklist in sync.
	taskCtx := tool.NewTaskContext()
	tk.AddGroup("todo",
		tool.TaskCreateTool(),
		tool.TaskGetTool(),
		tool.TaskListTool(),
		tool.TaskUpdateTool(),
	)

	// M7c: LSP tool. Lazy — spawns gopls/tsserver/pylsp on first call, rooted
	// at cwd so goToDefinition / findReferences / hover / symbols work over
	// the current project. Ships builtin defaults for go/typescript/python.
	tk.AddGroup("lsp", tool.LSPTool(tool.WithLSPRootDir(cwd)))

	// M7d: WebFetch + NotebookEdit — not in NewEnhancedToolkit but ship for
	// claude-code parity. WebFetch pulls a URL into context (per-tool timeout
	// + SSRF guard built into agentscope); NotebookEdit does cell-level edits
	// on Jupyter notebooks.
	tk.AddGroup("extra", tool.WebFetchTool(), tool.NotebookEditTool())

	skillsSection := ""
	if len(skillsList) > 0 {
		skillsSection = skill.FormatSkillInstructions(skillsList)
	}
	sysPrompt := buildSystemPrompt(cwd, tk, loadMemoryFiles(cwd), skillsSection)

	// resolve session + state.Context: resume/continue load history; fresh
	// starts empty. The system prompt is NOT in state.Context — agentscope
	// prepends its own from sysPrompt (dropLeadingSystem strips a legacy
	// system-role conv[0] from old transcripts).
	var sess *session.Session
	var stateCtx []*message.Msg
	if cfg.Resume != "" {
		s, conv, err := session.Load(cfg.Resume)
		if err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		sess = s
		stateCtx = dropLeadingSystem(conv)
	} else if cfg.Continue {
		s, conv, err := session.Latest(cwd)
		if err != nil {
			return nil, fmt.Errorf("continue: %w", err)
		}
		sess = s
		stateCtx = dropLeadingSystem(conv)
	} else {
		s, _ := session.New(cwd, cfg.Model) // best-effort; nil on failure → no persistence
		sess = s
		if sess != nil {
			_ = sess.SaveMeta()
		}
	}

	state := &asagent.AgentState{Context: stateCtx}
	if sess != nil {
		state.SessionID = sess.ID
	} else {
		state.SessionID = agentscope.GenerateID()
	}

	e := &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
		configuredMode: configuredMode, maxIters: cfg.MaxIters,
		state: state, sysPrompt: sysPrompt, cfg: cfg,
		compressCfg: buildCompressConfig(cfg), session: sess,
		mcpClients: mcpClients, mcpServers: mcpServers, hookRunner: hookRunner, workspaceCloser: workspaceCloser,
		cwd: cwd, settings: settingsCfg, readCache: readCache,
		taskCtx: taskCtx, thinker: thk, efforter: ef,
		subagents: subTracker, skillsList: skillsList,
	}

	// Replay: set up recorder or replayer middleware.
	if cfg.RecordTape != "" {
		e.recorder = replay.NewRecorder()
	} else if cfg.ReplayTape != "" {
		tape, rerr := loadTapeFromFile(cfg.ReplayTape)
		if rerr != nil {
			return nil, fmt.Errorf("replay: %w", rerr)
		}
		e.recorder = replay.NewReplayer(tape)
	}

	// Audit: FileLogger writing to ~/.lathe/audit/<session-id>.jsonl.
	if state.SessionID != "" {
		auditDir := defaultAuditDir()
		if auditDir != "" {
			aPath := filepath.Join(auditDir, state.SessionID+".jsonl")
			al, aerr := audit.NewFileLogger(aPath)
			if aerr == nil {
				e.auditLogger = al
				e.auditPath = aPath
			}
		}
	}

	e.assembleAgent()
	return e, nil
}

// assembleAgent builds (or rebuilds) the UnifiedAgent from the Engine's fields.
// Called once at construction; SetModel re-invokes it to swap the model while
// preserving state.Context (passed back via WithState). It also rebuilds the
// TaskTool so the subagent uses the current chatModel (not a stale capture).
func (e *Engine) assembleAgent() {
	// Rebuild the TaskTool with the current chatModel so SetModel propagates.
	// AddGroup replaces an existing group of the same name, so this is safe to
	// call on every rebuild.
	if e.toolkit != nil && e.subagents != nil {
		subTk := tool.NewEnhancedToolkit()
		e.toolkit.AddGroup("task", NewTaskToolWithTracker(e.chatModel, e.permEng, e.maxIters, subTk, e.subagents))
	}

	// Build middleware chain dynamically. Onion order: index 0 is outermost
	// (first to enter on request, last to exit on response).
	//   - CostTracker FIRST (outermost): rejects early before model call.
	//   - Product middlewares: role fixup + shell hooks.
	//   - Recorder: captures already-redacted output (sits outside guardrail).
	//   - Guardrail LAST (innermost): filters response before anything else.
	mws := make([]middleware.Middleware, 0, 6)

	// Spend cap (CostTrackerMiddleware) — outermost so it rejects before
	// any inner middleware or the core model call runs.
	if e.cfg != nil && e.cfg.MaxCostUSD > 0 {
		prices := buildModelPrices()
		ctOpts := []middleware.CostTrackerOption{middleware.WithMaxCostUSD(e.cfg.MaxCostUSD)}
		mws = append(mws, middleware.NewCostTrackerMiddleware(prices, ctOpts...))
	}

	// Product middlewares.
	mws = append(mws,
		&toolResultRoleMiddleware{BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "lathe-toolresult-role"}},
		&shellHookMiddleware{BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "lathe-shellhook"}, e: e},
	)

	// Replay recorder or replayer — between product and guardrail so it
	// captures the guardrail-redacted output (onion: response flows from
	// guardrail outward through recorder).
	if e.recorder != nil {
		mws = append(mws, e.recorder)
	}

	// Output guardrails — innermost (last). On response, the guardrail
	// processes the raw model output first; all outer middlewares
	// (including the recorder) see the already-filtered result.
	mws = append(mws, middleware.NewGuardrailMiddleware(
		middleware.KeywordRedactRule("secret-patterns", "[REDACTED]",
			"AKIA", "ghp_", "gho_", "ghs_"),
		secretSkRedactRule(),
		middleware.MaxLengthRule("max-output-length", 50000, middleware.GuardrailWarn),
	))

	opts := []asagent.AgentOption{
		asagent.WithToolkit(e.toolkit),
		asagent.WithReadCache(e.readCache),
		asagent.WithContextConfig(toContextConfig(e.compressCfg)),
		asagent.WithReactConfig(asagent.ReactConfig{MaxIters: e.maxIters}),
		asagent.WithMiddlewares(mws...),
		asagent.WithState(e.state),
		asagent.WithLoopHooks(&persistHook{e: e, saved: len(e.state.Context)}),
	}
	if e.permEng != nil {
		opts = append(opts, asagent.WithPermissionContext(e.permEng.Context))
	}
	e.agent = asagent.NewUnifiedAgent(e.name, e.sysPrompt, e.chatModel, opts...)
}

// secretSkRedactRule matches "sk-" followed by 20+ alphanumeric/dash/underscore
// characters, typical of OpenAI/Stripe/generic secret keys.
func secretSkRedactRule() middleware.GuardrailRule {
	return middleware.CustomRule("secret-sk-pattern", middleware.GuardrailRedact,
		func(text string) (bool, string) {
			// Simple linear scan: look for "sk-" prefix + 20 key chars.
			idx := 0
			for {
				pos := strings.Index(text[idx:], "sk-")
				if pos < 0 {
					return false, ""
				}
				start := idx + pos + 3
				count := 0
				for i := start; i < len(text); i++ {
					c := text[i]
					if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
						(c >= '0' && c <= '9') || c == '-' || c == '_' {
						count++
					} else {
						break
					}
				}
				if count >= 20 {
					return true, "contains secret key pattern (sk-...)"
				}
				idx = start
			}
		})
}

// buildModelPrices translates internal/pricing rates into the middleware
// ModelPrice map expected by CostTrackerMiddleware.
func buildModelPrices() map[string]middleware.ModelPrice {
	// pricing.Lookup is prefix-based; enumerate known prefixes directly from
	// the pricing table. The middleware does exact-match on model name, so we
	// also map them here for the most common model names.
	known := []struct {
		provider string
		model    string
	}{
		{"anthropic", "claude-opus-4"},
		{"anthropic", "claude-sonnet-4"},
		{"anthropic", "claude-haiku-4"},
		{"anthropic", "claude-3-5-sonnet"},
		{"anthropic", "claude-3-5-haiku"},
		{"anthropic", "claude-fable-5"},
		{"openai", "gpt-4o"},
		{"openai", "gpt-4o-mini"},
		{"openai", "o1"},
		{"openai", "o1-mini"},
		{"dashscope", "qwen-plus"},
		{"dashscope", "qwen-max"},
		{"dashscope", "qwen-turbo"},
	}
	prices := make(map[string]middleware.ModelPrice, len(known))
	for _, k := range known {
		r, ok := pricing.Lookup(k.provider, k.model)
		if !ok || r.Zero() {
			continue
		}
		prices[k.model] = middleware.ModelPrice{
			InputPerMillion:  r.InPerMTok,
			OutputPerMillion: r.OutPerMTok,
			CacheReadPerM:    r.CacheReadPerMTok,
			CacheWritePerM:   r.CacheWritePerMTok,
		}
	}
	return prices
}

// toContextConfig maps lathe's compressConfig to agentscope's ContextConfig
// (agentscope owns auto-compact; lathe's compressConfig stays as the source of
// defaults and for CompressNow). Defaults mirror agentscope's withDefaults.
func toContextConfig(cfg compressConfig) *asagent.ContextConfig {
	return &asagent.ContextConfig{
		TriggerRatio:      cfg.TriggerRatio,
		ReserveRatio:      cfg.ReserveRatio,
		ContextSize:       cfg.ContextSize,
		CompressionPrompt: cfg.CompressionPrompt,
		SummaryTemplate:   cfg.SummaryTemplate,
		SummarySchema:     cfg.SummarySchema,
		ToolResultLimit:   cfg.ToolResultLimit,
	}
}

// dropLeadingSystem drops a leading system-role message so a resumed legacy
// transcript (which stored the system prompt as conv[0]) does not double up
// with the system prompt agentscope prepends in prepareModelInput.
func dropLeadingSystem(conv []*message.Msg) []*message.Msg {
	if len(conv) > 0 && conv[0] != nil && conv[0].Role == message.RoleSystem {
		return conv[1:]
	}
	return conv
}

// newEngineForTest wires an Engine with an injected model/toolkit/engine.
func newEngineForTest(cm model.ChatModel, tk *tool.Toolkit, eng *permission.Engine, maxIters int) *Engine {
	initAgentscope()
	configuredMode := eng.Context.Mode
	// Hermetic tests are non-interactive: collapse default/accept_edits to
	// dont_ask (no-op for the bypass engines the tests actually inject).
	eng.Context.Mode = printEffectiveMode(configuredMode)
	e := &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: eng,
		configuredMode: configuredMode, maxIters: maxIters,
		state:       &asagent.AgentState{SessionID: agentscope.GenerateID()},
		sysPrompt:   buildSystemPrompt("", tk, "", ""),
		cfg:         &config.Config{Provider: "openai", Model: "test-model", APIKey: "k"},
		compressCfg: defaultCompressConfig(),
		readCache:   tool.NewReadCache(0, 0),
	}
	e.assembleAgent()
	return e
}

// newSubagentEngine builds a non-interactive nested Engine for the Task tool.
// It shares the parent's chat model + a copy of the parent's permission Context
// (rule maps shared, mode forced to the print-effective mode so an Ask cannot
// hang). M6b will replace this with UnifiedAgent.Spawn.
func newSubagentEngine(name, sysPrompt string, cm model.ChatModel, tk *tool.Toolkit, parentPermEng *permission.Engine, maxIters int) *Engine {
	initAgentscope()
	e := &Engine{
		name: name, chatModel: cm, toolkit: tk,
		maxIters: maxIters, cfg: &config.Config{},
		compressCfg: defaultCompressConfig(),
		state:       &asagent.AgentState{SessionID: agentscope.GenerateID()},
		sysPrompt:   sysPrompt, readCache: tool.NewReadCache(0, 0),
	}
	if parentPermEng != nil {
		ctxCopy := *parentPermEng.Context // shallow copy: shares rule maps, separate Mode
		ctxCopy.Mode = printEffectiveMode(ctxCopy.Mode)
		e.permEng = permission.NewEngine(&ctxCopy)
		e.configuredMode = parentPermEng.Context.Mode
	}
	e.assembleAgent()
	return e
}

// SetModel switches the chat model (same provider, new model name). The agent
// is rebuilt (same state.Context/toolkit) so the new model takes effect on the
// next Run. Unknown model names are accepted (the API layer reports the error
// on next call). M7a: the thinker's enable/budget carry across the switch so a
// live `/thinking on` isn't lost when the user runs `/model foo`.
func (e *Engine) SetModel(name string) error {
	e.cfg.Model = name
	cm, err := buildChatModel(e.cfg)
	if err != nil {
		return err
	}
	en, bud := false, e.cfg.ThinkingBudget
	if e.thinker != nil {
		en, bud = e.thinker.Thinking()
	}
	e.thinker = newThinker(cm, en, bud)
	lvl := e.cfg.ReasoningEffort
	if e.efforter != nil {
		lvl = e.efforter.Effort()
	}
	e.efforter = newEfforter(e.thinker, lvl)
	e.chatModel = e.efforter
	e.assembleAgent() // rebuild ucm + agent; persistHook.saved = len(state.Context)
	return nil
}

// SetThinking toggles extended thinking + budget (M7a). Used by /thinking.
// A budget of 0 preserves the current budget so `/thinking on` after a prior
// `/thinking budget=8000` re-enables at 8000 rather than resetting.
func (e *Engine) SetThinking(enable bool, budget int) {
	if e.thinker == nil {
		return
	}
	e.thinker.SetThinking(enable, budget)
}

// Thinking returns the current enable flag + budget for the /thinking slash.
func (e *Engine) Thinking() (bool, int) {
	if e.thinker == nil {
		return false, 0
	}
	return e.thinker.Thinking()
}

// SetEffort updates the reasoning-effort level ("" disables) (M7b). Used by
// /effort.
func (e *Engine) SetEffort(level string) {
	if e.efforter == nil {
		return
	}
	e.efforter.SetEffort(level)
}

// Effort returns the current reasoning-effort level ("" when off).
func (e *Engine) Effort() string {
	if e.efforter == nil {
		return ""
	}
	return e.efforter.Effort()
}

// EnterPlanMode swaps the permission mode to ModeExplore (read-only) and
// remembers the previous mode so ExitPlanMode can restore it (M7g). Idempotent
// — a second EnterPlanMode call is a no-op so it does not overwrite the saved
// mode with Explore.
func (e *Engine) EnterPlanMode() {
	if e.planActive || e.permEng == nil || e.permEng.Context == nil {
		return
	}
	e.prePlanMode = e.permEng.Context.Mode
	e.permEng.Context.Mode = permission.ModeExplore
	e.planActive = true
}

// ExitPlanMode restores the permission mode saved by EnterPlanMode (M7g).
// Idempotent — called with plan not active it is a no-op.
func (e *Engine) ExitPlanMode() {
	if !e.planActive || e.permEng == nil || e.permEng.Context == nil {
		return
	}
	e.permEng.Context.Mode = e.prePlanMode
	e.planActive = false
}

// ApprovePlan exits plan mode and switches to accept_edits so the model can
// immediately begin executing the approved plan without per-file prompts
// (M10b). No-op when plan mode is not active.
func (e *Engine) ApprovePlan() {
	if !e.planActive || e.permEng == nil || e.permEng.Context == nil {
		return
	}
	e.permEng.Context.Mode = permission.ModeAcceptEdits
	e.planActive = false
}

// IsPlanMode reports whether the engine is currently in plan mode (M7g).
func (e *Engine) IsPlanMode() bool { return e.planActive }

// PermissionMode returns the effective permission mode string (M10c).
func (e *Engine) PermissionMode() string {
	if e.permEng == nil || e.permEng.Context == nil {
		return string(permission.ModeDefault)
	}
	return string(e.permEng.Context.Mode)
}

// SetPermissionMode sets the effective permission mode (M10c).
func (e *Engine) SetPermissionMode(mode string) {
	if e.permEng == nil || e.permEng.Context == nil {
		return
	}
	e.permEng.Context.Mode = permission.PermissionMode(mode)
}

// Subagents returns a snapshot of subagent dispatches recorded by the Task
// tool during this session (M7e). Used by /agents.
func (e *Engine) Subagents() []subagent.SubagentInfo {
	if e.subagents == nil {
		return nil
	}
	return e.subagents.List()
}

// Jailed reports whether file tools are confined to cwd via
// tool.WithWorkspaceRoot injected in Run's ctx (M7f).
func (e *Engine) Jailed() bool {
	if e.cfg == nil {
		return false
	}
	return e.cfg.Jail
}

// SandboxMode returns "host" when cfg.Sandbox is empty, else the raw mode
// string ("docker"/"e2b"). Used by /sandbox for a quick posture report.
func (e *Engine) SandboxMode() string {
	if e.cfg == nil || e.cfg.Sandbox == "" {
		return "host"
	}
	return e.cfg.Sandbox
}

// SkillsList returns the skills discovered at NewEngine time (M8c). Used by
// /skills. Empty when no ~/.lathe/skills or <cwd>/.lathe/skills entries.
func (e *Engine) SkillsList() []skill.Skill { return e.skillsList }

// HooksList returns the settings.json hook map (event → matchers) wired at
// startup (M8c). Used by /hooks. Empty map when no hooks are configured.
func (e *Engine) HooksList() map[string][]settings.Matcher {
	if e.settings == nil {
		return nil
	}
	return e.settings.Hooks
}

// AgentscopeVersion returns the agentscope-go dependency version as reported
// by debug.ReadBuildInfo (M9a). Best-effort: returns "unknown" when build info
// is unavailable (e.g. under `go run` without module info) or when the module
// path is not in the dependency graph. Used by /doctor.
func (e *Engine) AgentscopeVersion() string { return agentscopeVersion() }

// agentscopeVersion is a package-level helper so tests can call it directly.
func agentscopeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range info.Deps {
		if d == nil {
			continue
		}
		if strings.HasPrefix(d.Path, "github.com/alanfokco/agentscope-go") {
			if d.Replace != nil {
				return d.Replace.Version + " (replaced)"
			}
			return d.Version
		}
	}
	return "unknown"
}

// ListModels returns model names available for the current provider.
func (e *Engine) ListModels() []string {
	var out []string
	for _, c := range model.ListModels() {
		if c.Provider == e.cfg.Provider {
			out = append(out, c.Name)
		}
	}
	return out
}

// ModelName returns the current model name.
func (e *Engine) ModelName() string { return e.cfg.Model }

// StatusInfo returns the session/cwd/context-size snapshot used to build a
// statusline payload (M5b). session fields are "" when no session is active.
func (e *Engine) StatusInfo() (cwd, sessionID, transcriptPath string, contextSize int) {
	cwd = e.cwd
	if e.session != nil {
		sessionID = e.session.ID
		transcriptPath = e.session.Path
	}
	contextSize = e.compressCfg.ContextSize
	return
}

// GitInfo returns the current git branch and dirty state. M11c.
func (e *Engine) GitInfo() (branch string, dirty bool, ok bool) {
	return gitInfo(e.cwd)
}

// StatusLineConfig returns the parsed statusLine setting, or nil if unset (M5b).
func (e *Engine) StatusLineConfig() *settings.StatusLineConfig {
	if e.settings == nil {
		return nil
	}
	return e.settings.StatusLine
}

// MCPServers returns a summary of configured MCP servers (name + tool count),
// snapshotted from mcpconfig.Load at NewEngine (M6c-5). Empty when no MCP
// servers are configured. Used by /mcp.
func (e *Engine) MCPServers() []mcpconfig.ServerInfo { return e.mcpServers }

// ListSessions returns summaries of the current cwd's sessions, newest first
// (M6c-5). Best-effort: empty on missing/unreadable project dir. Used by
// /resume to render a list + `lathe --resume <id>` hint (in-process reload
// is deferred).
func (e *Engine) ListSessions() []session.Summary { return session.List(e.cwd) }

// ToolNames returns the names of every tool exposed to the model (M6f), sorted
// alphabetically. Used by /tools to give the user visibility into the active
// toolkit — builtin (Bash/Read/Edit/MultiEdit/ApplyPatch/…), MCP-discovered,
// skills viewer, and the Task subagent tool all end up here.
func (e *Engine) ToolNames() []string {
	schemas := e.toolkit.GetToolSchemas()
	if len(schemas) == 0 {
		return nil
	}
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Function.Name)
	}
	sort.Strings(out)
	return out
}

// Close releases engine resources, including MCP client connections. It is
// idempotent and best-effort (per-client errors are ignored).
func (e *Engine) Close() error {
	for _, c := range e.mcpClients {
		if c != nil {
			_ = c.Close()
		}
	}
	e.mcpClients = nil
	if e.workspaceCloser != nil {
		_ = e.workspaceCloser()
	}
	e.workspaceCloser = nil
	// Close the audit file logger if present.
	if fl, ok := e.auditLogger.(*audit.FileLogger); ok && fl != nil {
		_ = fl.Close()
	}
	e.auditLogger = nil
	return nil
}

// SaveRecording marshals the recorder's tape to the configured file. Called
// from the TUI cleanup path.
func (e *Engine) SaveRecording() error {
	if e.recorder == nil || e.cfg == nil || e.cfg.RecordTape == "" {
		return nil
	}
	tape := e.recorder.Tape()
	if tape == nil || len(tape.Entries) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(tape, "", "  ")
	if err != nil {
		return fmt.Errorf("save recording: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(e.cfg.RecordTape), 0o755); err != nil {
		return fmt.Errorf("save recording: mkdir: %w", err)
	}
	return os.WriteFile(e.cfg.RecordTape, data, 0o644)
}

// AuditPath returns the path to the audit log for this session, or "" if
// audit logging is not active.
func (e *Engine) AuditPath() string { return e.auditPath }

// loadTapeFromFile reads and unmarshals a replay tape from a JSON file.
func loadTapeFromFile(path string) (*replay.Tape, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tape replay.Tape
	if err := json.Unmarshal(data, &tape); err != nil {
		return nil, err
	}
	return &tape, nil
}

// defaultAuditDir returns ~/.lathe/audit, creating it if needed.
func defaultAuditDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".lathe", "audit")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// initAgentscope initializes agentscope and dampens its log verbosity. v3
// imports agentscope's agent package, whose httpx client logs every request at
// DEBUG (forwarded to stdout via the LogrusToSlogHook, which bypasses the slog
// level filter by calling Handler.Handle directly). Setting the logrus level to
// INFO drops those DEBUG entries before the hook fires (logrus filters before
// firing hooks), keeping lathe's stdout — especially stream-json NDJSON — clean.
func initAgentscope() {
	agentscope.Init()
	agentscope.Log().SetLevel(logrus.InfoLevel)
}

func buildChatModel(cfg *config.Config) (model.ChatModel, error) {
	var cm model.ChatModel
	var err error
	switch cfg.Provider {
	case "anthropic":
		cm, err = model.NewAnthropicChatModel(&model.AnthropicConfig{
			SecretAPIKey: model.NewSecretStr(cfg.APIKey), BaseURL: cfg.BaseURL, Model: cfg.Model, MaxOutputTokens: 8192,
			PromptCaching: cfg.PromptCaching, // M8a
		})
	case "openai":
		cm, err = model.NewOpenAIChatModel(model.OpenAIConfig{
			SecretAPIKey: model.NewSecretStr(cfg.APIKey), BaseURL: cfg.BaseURL, Model: cfg.Model,
		})
	case "dashscope":
		cm, err = model.NewDashScopeChatModel(model.DashScopeConfig{
			SecretAPIKey: model.NewSecretStr(cfg.APIKey), BaseURL: cfg.BaseURL, Model: cfg.Model,
		})
	case "ollama":
		cm, err = model.NewOpenAIChatModel(model.OpenAIConfig{
			SecretAPIKey: model.NewSecretStr(cfg.APIKey),
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
	if err != nil {
		return nil, err
	}
	// M6a: decorate with resilience (circuit breaker + optional rate limit) so a
	// transient provider/network error retries/short-circuits instead of aborting
	// the whole turn.
	return applyResilience(cm, cfg), nil
}

// applyResilience wraps cm with a circuit breaker (and an optional rate limiter
// when RateLimitPerSec > 0). With no knobs set it still installs a default
// breaker. Returns a model.ChatModel — the same interface the Engine holds, so
// no Engine changes are needed.
func applyResilience(cm model.ChatModel, cfg *config.Config) model.ChatModel {
	threshold := cfg.CircuitBreakerThreshold
	if threshold <= 0 {
		threshold = 5
	}
	opts := []resilience.ResilienceOption{
		resilience.WithCircuitBreaker(resilience.NewCircuitBreaker(threshold, 30*time.Second)),
	}
	if cfg.RateLimitPerSec > 0 {
		opts = append(opts, resilience.WithRateLimit(resilience.NewRateLimiter(cfg.RateLimitPerSec, cfg.RateBurst)))
	}
	return resilience.Wrap(cm, opts...)
}
