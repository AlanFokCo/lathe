package agent

import (
	"context"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

// shellHookMiddleware wires lathe's settings.json PreToolUse/PostToolUse shell
// hooks into the agentscope acting pipeline (M6b). v3 deleted dispatch.go, so
// these hooks stopped firing — this middleware restores them:
//   - PreToolUse runs before the tool executes; a {"decision":"block"} result
//     short-circuits with a denied ToolResponse (mirrors the old dispatch).
//   - PostToolUse runs after; {"additionalContext":"..."} is appended to the
//     tool output (a TextBlock on the ToolResponse, which agentscope extracts
//     into the ToolResult text + saved context).
//
// It holds a back-reference to the Engine so it reads e.hookRunner live (tests
// assign hookRunner after construction; nil-runner Run is safe → no-op).
type shellHookMiddleware struct {
	middleware.BaseMiddleware
	e *Engine
}

func (m *shellHookMiddleware) OnActing(ctx context.Context, input *middleware.ActingInput, next middleware.ActingHandler) (*tool.ToolResponse, error) {
	name := input.ToolCall.Name
	parsed, _ := input.ToolCall.ParseInput()
	if r, _ := m.e.hookRunner.Run(ctx, "PreToolUse", map[string]any{"tool_name": name, "tool_input": parsed}); r.Block {
		msg := "Permission denied: blocked by hook"
		if r.Reason != "" {
			msg += ": " + r.Reason
		}
		return &tool.ToolResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: msg}},
			State:   message.ToolResultDenied,
		}, nil
	}
	resp, err := next(ctx, input)
	if err != nil || resp == nil {
		return resp, err
	}
	if r, _ := m.e.hookRunner.Run(ctx, "PostToolUse", map[string]any{
		"tool_name":   name,
		"tool_input":  parsed,
		"tool_output": extractToolOutput(resp),
	}); r.Context != "" {
		resp.Content = append(resp.Content, message.TextBlock{Type: "text", Text: "\n\n" + r.Context})
	}
	return resp, nil
}

// extractToolOutput concatenates the TextBlock content of a ToolResponse (used
// for the PostToolUse hook payload).
func extractToolOutput(resp *tool.ToolResponse) string {
	var out string
	for _, b := range resp.Content {
		if tb, ok := b.(message.TextBlock); ok {
			out += tb.Text
		}
	}
	return out
}
