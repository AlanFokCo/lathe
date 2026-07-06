package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	asevent "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/subagent"
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
	tracker    *subagent.Tracker // M7e: optional lifecycle recorder (nil = no tracking)
}

// NewTaskTool builds a Task tool without lifecycle tracking. Kept for the
// existing test suite; production callers should prefer NewTaskToolWithTracker.
func NewTaskTool(cm model.ChatModel, permEng *permission.Engine, maxIters int, subToolkit *tool.Toolkit) tool.Tool {
	return NewTaskToolWithTracker(cm, permEng, maxIters, subToolkit, nil)
}

// NewTaskToolWithTracker builds a Task tool that records subagent dispatches
// into tracker (M7e). tracker may be nil, in which case behaviour matches
// NewTaskTool.
func NewTaskToolWithTracker(cm model.ChatModel, permEng *permission.Engine, maxIters int, subToolkit *tool.Toolkit, tracker *subagent.Tracker) tool.Tool {
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
		tracker:    tracker,
	}
}

// Execute spawns the subagent, runs it to completion, and returns its
// accumulated text output (all TextDelta across turns; tool results are not
// included). It blocks until the subagent finishes (end_turn/max_iters/
// cancelled/error) or the ctx is cancelled. M7e: when a tracker is attached,
// records the dispatch with the "description" arg as the label and marks
// completion with output length so /agents can show what happened.
func (t *TaskTool) Execute(ctx context.Context, input map[string]any) (*tool.ToolResponse, error) {
	prompt, _ := input["prompt"].(string)
	if prompt == "" {
		return tool.NewErrorResponse(fmt.Errorf("prompt is required")), nil
	}
	description, _ := input["description"].(string)
	var trackID string
	if t.tracker != nil {
		trackID = t.tracker.Start(description)
	}
	sub := newSubagentEngine("lathe-subagent", subagentSysPrompt, t.chatModel, t.subToolkit, t.permEng, t.maxIters)
	ch := sub.Run(ctx, prompt)
	var text strings.Builder
	for ev := range ch {
		if td, ok := ev.(asevent.TextBlockDeltaEvent); ok {
			text.WriteString(td.Delta)
		}
	}
	out := text.String()
	if t.tracker != nil {
		t.tracker.Complete(trackID, "completed", len(out))
	}
	return tool.NewTextResponse(out), nil
}

// CheckPermissions auto-allows the Task tool (it is a meta-tool; the subagent's
// own tools are permission-gated via the sub's permEng).
func (t *TaskTool) CheckPermissions(_ map[string]any, _ *permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.BehaviorAllow, Message: "auto-allowed: Task subagent dispatch"}
}
