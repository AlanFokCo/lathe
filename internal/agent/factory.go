package agent

import (
	"context"
	"fmt"
	"os"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/mcp"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/hooks"
	"github.com/alanfokco/lathe/internal/mcpconfig"
	"github.com/alanfokco/lathe/internal/skills"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
	"github.com/alanfokco/lathe/internal/workspace"
)

// Engine is lathe's turn engine. It is NOT a wrapper around UnifiedAgent;
// it drives model.ChatStream directly in its own loop.
type Engine struct {
	name        string
	chatModel   model.ChatModel
	toolkit     *tool.Toolkit
	permEng     *permission.Engine
	maxIters    int
	conv        []*message.Msg
	cfg         *config.Config
	compressCfg compressConfig
	session     *session.Session
	interactive bool
	approvalCh  chan string
	mcpClients      []mcp.Client
	hookRunner      *hooks.Runner
	workspaceCloser func() error
}

// NewEngine assembles an Engine from a resolved config (production path:
// builds a real ChatModel for the configured provider). The system prompt is
// built once at construction (env + tool descriptions + project memory).
func NewEngine(ctx context.Context, cfg *config.Config) (*Engine, error) {
	agentscope.Init()
	cm, err := buildChatModel(cfg)
	if err != nil {
		return nil, err
	}
	tk := tool.NewEnhancedToolkit()
	permCtx := permission.NewContext(permission.PermissionMode(cfg.Permission))
	permEng := permission.NewEngine(permCtx)

	// M4a: discover skills (user ~/.lathe/skills + project .lathe/skills walk-up).
	// Registered here so all paths (new/resume/continue) expose the Skill tool.
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
		workspaceCloser = closer
		subToolkit = workspace.WorkspaceToolkit(ws)
	} else {
		subToolkit = tool.NewEnhancedToolkit()
	}

	// M4d: Task subagent tool (spawns a nested lathe Engine with a builtins-only
	// toolkit — no Task, so the subagent cannot recurse). In sandbox mode the
	// subagent shares the same workspace (no escape).
	tk.AddGroup("task", NewTaskTool(cm, permEng, cfg.MaxIters, subToolkit))

	// resume an existing session?
	if cfg.Resume != "" {
		sess, conv, err := session.Load(cfg.Resume)
		if err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		return &Engine{
			name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
			maxIters: cfg.MaxIters, cfg: cfg, compressCfg: defaultCompressConfig(),
			conv: conv, session: sess, mcpClients: mcpClients, hookRunner: hookRunner, workspaceCloser: workspaceCloser,
		}, nil
	}
	if cfg.Continue {
		sess, conv, err := session.Latest(mustCwd())
		if err != nil {
			return nil, fmt.Errorf("continue: %w", err)
		}
		return &Engine{
			name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
			maxIters: cfg.MaxIters, cfg: cfg, compressCfg: defaultCompressConfig(),
			conv: conv, session: sess, mcpClients: mcpClients, hookRunner: hookRunner, workspaceCloser: workspaceCloser,
		}, nil
	}

	skillsSection := ""
	if len(skillsList) > 0 {
		skillsSection = skill.FormatSkillInstructions(skillsList)
	}
	sysMsg := message.SystemMsg("lathe", buildSystemPrompt(cwd, tk, loadMemoryFiles(cwd), skillsSection))
	sess, _ := session.New(cwd, cfg.Model) // best-effort; nil on failure → no persistence
	e := &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
		maxIters: cfg.MaxIters, cfg: cfg, compressCfg: defaultCompressConfig(),
		session: sess, approvalCh: make(chan string, 1), mcpClients: mcpClients, hookRunner: hookRunner, workspaceCloser: workspaceCloser,
	}
	if sess != nil {
		_ = sess.SaveMeta()
	}
	e.appendConv(sysMsg)
	return e, nil
}

// newEngineForTest wires an Engine with an injected model/toolkit/engine.
func newEngineForTest(cm model.ChatModel, tk *tool.Toolkit, eng *permission.Engine, maxIters int) *Engine {
	agentscope.Init()
	return &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: eng,
		maxIters:    maxIters,
		conv:        []*message.Msg{message.SystemMsg("lathe", buildSystemPrompt("", tk, "", ""))},
		cfg:         &config.Config{Provider: "openai", Model: "test-model", APIKey: "k"},
		compressCfg: defaultCompressConfig(),
		approvalCh:  make(chan string, 1),
	}
}

// SetModel switches the chat model (same provider, new model name). The new
// model is rebuilt from the stored config; conversation history is preserved.
// Unknown model names are accepted (the API layer reports the error on next call).
func (e *Engine) SetModel(name string) error {
	e.cfg.Model = name
	cm, err := buildChatModel(e.cfg)
	if err != nil {
		return err
	}
	e.chatModel = cm
	return nil
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

// SetInteractive enables (TUI) or disables (print) interactive approval.
func (e *Engine) SetInteractive(b bool) { e.interactive = b }

// SubmitApproval delivers the user's approval decision ("allow"/"deny"/"always")
// to unblock a paused dispatch. Called by the TUI after a RequireApproval event.
func (e *Engine) SubmitApproval(decision string) {
	select {
	case e.approvalCh <- decision:
	default:
		// no pending approval; drop (TUI state machine prevents stray calls)
	}
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
	return nil
}

func buildChatModel(cfg *config.Config) (model.ChatModel, error) {
	switch cfg.Provider {
	case "anthropic":
		return model.NewAnthropicChatModel(&model.AnthropicConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model, MaxOutputTokens: 8192,
		})
	case "openai":
		return model.NewOpenAIChatModel(model.OpenAIConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model,
		})
	case "dashscope":
		return model.NewDashScopeChatModel(model.DashScopeConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model,
		})
	case "ollama":
		return model.NewOpenAIChatModel(model.OpenAIConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	}
	return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
}
