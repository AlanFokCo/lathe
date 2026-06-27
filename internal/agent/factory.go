package agent

import (
	"fmt"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/session"
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
}

// NewEngine assembles an Engine from a resolved config (production path:
// builds a real ChatModel for the configured provider). The system prompt is
// built once at construction (env + tool descriptions + project memory).
func NewEngine(cfg *config.Config) (*Engine, error) {
	agentscope.Init()
	cm, err := buildChatModel(cfg)
	if err != nil {
		return nil, err
	}
	tk := tool.NewEnhancedToolkit()
	permCtx := permission.NewContext(permission.PermissionMode(cfg.Permission))
	permEng := permission.NewEngine(permCtx)

	// resume an existing session?
	if cfg.Resume != "" {
		sess, conv, err := session.Load(cfg.Resume)
		if err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		return &Engine{
			name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
			maxIters: cfg.MaxIters, cfg: cfg, compressCfg: defaultCompressConfig(),
			conv: conv, session: sess,
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
			conv: conv, session: sess,
		}, nil
	}

	cwd := mustCwd()
	sysMsg := message.SystemMsg("lathe", buildSystemPrompt(cwd, tk, loadMemoryFiles(cwd)))
	sess, _ := session.New(cwd, cfg.Model) // best-effort; nil on failure → no persistence
	e := &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
		maxIters: cfg.MaxIters, cfg: cfg, compressCfg: defaultCompressConfig(),
		session: sess, approvalCh: make(chan string, 1),
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
		conv:        []*message.Msg{message.SystemMsg("lathe", buildSystemPrompt("", tk, ""))},
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
	}
	return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
}
