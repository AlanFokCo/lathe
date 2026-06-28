package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/event"
)

var taskToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"description": {"type": "string", "description": "A short description of what the subagent should do (shown to the user)."},
		"prompt": {"type": "string", "description": "The task prompt for the subagent."}
	},
	"required": ["description", "prompt"]
}`)

// subagentSysPrompt is the system prompt for spawned subagents.
const subagentSysPrompt = "You are a lathe subagent, spawned by the parent agent to complete a focused task. " +
	"Use the available tools to do the work; do not ask for clarification. " +
	"When done, give a concise summary of what you did and what you found."

// TaskTool spawns a nested lathe Engine (subagent) to complete a task and
// returns the subagent's text output. The subagent uses the parent's chat
// model + permission engine (non-interactive) and a restricted toolkit
// (builtins only — no Task, so no recursion).
type TaskTool struct {
	tool.BaseTool
	chatModel  model.ChatModel
	permEng    *permission.Engine
	maxIters   int
	subToolkit *tool.Toolkit
}

// NewTaskTool builds a Task tool. subToolkit is the toolkit the subagent uses
// (production: a fresh NewEnhancedToolkit; tests: an injected toolkit).
func NewTaskTool(cm model.ChatModel, permEng *permission.Engine, maxIters int, subToolkit *tool.Toolkit) tool.Tool {
	return &TaskTool{
		BaseTool: tool.BaseTool{
			ToolName:        "Task",
			ToolDescription: "Spawn a subagent to complete a focused task. Returns the subagent's text output.",
			ToolSchema:      taskToolSchema,
		},
		chatModel:  cm,
		permEng:    permEng,
		maxIters:   maxIters,
		subToolkit: subToolkit,
	}
}

// Execute spawns the subagent, runs it to completion, and returns its
// accumulated text output (all TextDelta across turns; tool results are not
// included). It blocks until the subagent finishes (end_turn/max_iters/
// cancelled/error) or the ctx is cancelled.
func (t *TaskTool) Execute(ctx context.Context, input map[string]any) (*tool.ToolResponse, error) {
	prompt, _ := input["prompt"].(string)
	if prompt == "" {
		return tool.NewErrorResponse(fmt.Errorf("prompt is required")), nil
	}
	sub := &Engine{
		name:        "lathe-subagent",
		chatModel:   t.chatModel,
		toolkit:     t.subToolkit,
		permEng:     t.permEng,
		maxIters:    t.maxIters,
		conv:        []*message.Msg{message.SystemMsg("lathe-subagent", subagentSysPrompt)},
		cfg:         &config.Config{},
		compressCfg: defaultCompressConfig(),
		approvalCh:  make(chan string, 1),
		interactive: false,
	}
	ch := sub.Run(ctx, prompt)
	var text strings.Builder
	for ev := range ch {
		if td, ok := ev.(event.TextDelta); ok {
			text.WriteString(td.Delta)
		}
	}
	return tool.NewTextResponse(text.String()), nil
}

// CheckPermissions auto-allows the Task tool (it is a meta-tool; the subagent's
// own tools are permission-gated via the sub's permEng).
func (t *TaskTool) CheckPermissions(_ map[string]any, _ *permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.BehaviorAllow, Message: "auto-allowed: Task subagent dispatch"}
}
