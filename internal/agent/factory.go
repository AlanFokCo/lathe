package agent

import (
	"fmt"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
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
	sysPrompt string
	maxIters  int
}

// NewEngine assembles an Engine from a resolved config (production path:
// builds a real ChatModel for the configured provider).
func NewEngine(cfg *config.Config) (*Engine, error) {
	agentscope.Init()
	cm, err := buildChatModel(cfg)
	if err != nil {
		return nil, err
	}
	tk := tool.NewEnhancedToolkit()
	permCtx := permission.NewContext(permission.PermissionMode(cfg.Permission))
	permEng := permission.NewEngine(permCtx)
	return &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: permEng,
		sysPrompt: systemPrompt(), maxIters: cfg.MaxIters,
	}, nil
}

// newEngineForTest wires an Engine with an injected model/toolkit/engine.
func newEngineForTest(cm model.ChatModel, tk *tool.Toolkit, eng *permission.Engine, maxIters int) *Engine {
	agentscope.Init()
	return &Engine{
		name: "lathe", chatModel: cm, toolkit: tk, permEng: eng,
		sysPrompt: systemPrompt(), maxIters: maxIters,
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
