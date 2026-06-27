package agent

import (
	"context"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/event"
)

// dispatch checks permissions and executes each tool call, returning
// ToolResultBlocks (aligned by index to calls) and emitting ToolCallStart +
// ToolResult events. In print mode, Ask is treated as Deny (no interactive
// prompt); M2 TUI will prompt instead.
//
// M1 executes calls sequentially; concurrency-safe batching is a M2 refinement.
func dispatch(
	ctx context.Context,
	calls []message.ToolCallBlock,
	tk *tool.Toolkit,
	eng *permission.Engine,
	emit func(event.Event),
) []message.ToolResultBlock {
	results := make([]message.ToolResultBlock, len(calls))
	for i, tc := range calls {
		results[i] = runOneTool(ctx, tc, tk, eng, emit)
	}
	return results
}

func runOneTool(ctx context.Context, tc message.ToolCallBlock, tk *tool.Toolkit, eng *permission.Engine, emit func(event.Event)) message.ToolResultBlock {
	t := tk.Get(tc.Name)
	emit(event.ToolCallStart{ID: tc.ID, Name: tc.Name, Input: tc.Input})

	if t == nil {
		tr := errorResult(tc, fmt.Sprintf("tool %q not found", tc.Name))
		emit(toolResultEvent(tc, tr, ""))
		return tr
	}

	if eng != nil {
		input, err := tc.ParseInput()
		if err != nil {
			tr := errorResult(tc, fmt.Sprintf("parse input: %v", err))
			emit(toolResultEvent(tc, tr, ""))
			return tr
		}
		decision, err := eng.CheckPermission(t, input)
		if err != nil {
			tr := errorResult(tc, fmt.Sprintf("permission: %v", err))
			emit(toolResultEvent(tc, tr, ""))
			return tr
		}
		switch decision.Behavior {
		case permission.BehaviorDeny, permission.BehaviorAsk:
			tr := deniedResult(tc, decision.Message)
			emit(toolResultEvent(tc, tr, ""))
			return tr
		}
	}

	resp, err := tk.CallToolFromBlock(ctx, &tc)
	if err != nil {
		tr := errorResult(tc, err.Error())
		emit(toolResultEvent(tc, tr, ""))
		return tr
	}
	out := ""
	for _, b := range resp.Content {
		if tb, ok := b.(message.TextBlock); ok {
			out += tb.Text
		}
	}
	diff, _ := resp.Metadata["diff"].(string)
	tr := message.ToolResultBlock{
		Type: "tool_result", ID: tc.ID, Name: tc.Name,
		Output: out, State: resp.State, Metadata: resp.Metadata,
	}
	emit(toolResultEvent(tc, tr, diff))
	return tr
}

func toolResultEvent(tc message.ToolCallBlock, tr message.ToolResultBlock, diff string) event.ToolResult {
	out, _ := tr.Output.(string)
	return event.ToolResult{ID: tc.ID, Name: tc.Name, Output: out, State: string(tr.State), Diff: diff}
}

func deniedResult(tc message.ToolCallBlock, msg string) message.ToolResultBlock {
	if msg == "" {
		msg = "denied"
	}
	return message.ToolResultBlock{
		Type: "tool_result", ID: tc.ID, Name: tc.Name,
		Output: "Permission denied: " + msg, State: message.ToolResultDenied,
	}
}

func errorResult(tc message.ToolCallBlock, msg string) message.ToolResultBlock {
	return message.ToolResultBlock{
		Type: "tool_result", ID: tc.ID, Name: tc.Name,
		Output: "Error: " + msg, State: message.ToolResultError,
	}
}
