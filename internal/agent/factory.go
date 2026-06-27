package agent

import (
	"fmt"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
)

// Engine is lathe's turn engine. It is NOT a wrapper around UnifiedAgent;
// it drives model.ChatStream directly in its own loop.
type Engine struct {
	name      string
	chatModel model.ChatModel
	toolkit   *tool.Toolkit
	permEng   *permission.Engine
	maxIters  int
	conv      []*message.Msg
	cfg       *config.Config
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
	cwd := mustCwd()
	return &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
		maxIters: cfg.MaxIters,
		conv:     []*message.Msg{message.SystemMsg("lathe", buildSystemPrompt(cwd, tk, loadMemoryFiles(cwd)))},
		cfg:      cfg,
	}, nil
}

// newEngineForTest wires an Engine with an injected model/toolkit/engine.
func newEngineForTest(cm model.ChatModel, tk *tool.Toolkit, eng *permission.Engine, maxIters int) *Engine {
	agentscope.Init()
	return &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: eng,
		maxIters: maxIters,
		conv:     []*message.Msg{message.SystemMsg("lathe", buildSystemPrompt("", tk, ""))},
		cfg:      &config.Config{Provider: "openai", Model: "test-model", APIKey: "k"},
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
