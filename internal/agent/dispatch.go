package agent

import (
	"context"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/event"
	"github.com/alanfokco/lathe/internal/hooks"
)

// dispatch checks permissions and executes each tool call, returning
// ToolResultBlocks (aligned by index to calls) and emitting ToolCallStart +
// ToolResult events. In print mode, Ask is treated as Deny (no interactive
// prompt); M2 TUI will prompt instead. runners is variadic so existing
// callers pass nothing (nil runner → no hooks); pass one *hooks.Runner to
// enable Pre/PostToolUse.
func dispatch(
	ctx context.Context,
	calls []message.ToolCallBlock,
	tk *tool.Toolkit,
	eng *permission.Engine,
	interactive bool,
	approvalCh chan string,
	emit func(event.Event),
	runners ...*hooks.Runner,
) []message.ToolResultBlock {
	var runner *hooks.Runner
	if len(runners) > 0 {
		runner = runners[0]
	}
	results := make([]message.ToolResultBlock, len(calls))
	for i, tc := range calls {
		results[i] = runOneTool(ctx, tc, tk, eng, interactive, approvalCh, emit, runner)
	}
	return results
}

func runOneTool(ctx context.Context, tc message.ToolCallBlock, tk *tool.Toolkit, eng *permission.Engine, interactive bool, approvalCh chan string, emit func(event.Event), runner *hooks.Runner) message.ToolResultBlock {
	t := tk.Get(tc.Name)
	emit(event.ToolCallStart{ID: tc.ID, Name: tc.Name, Input: tc.Input})

	if t == nil {
		tr := errorResult(tc, fmt.Sprintf("tool %q not found", tc.Name))
		emit(toolResultEvent(tc, tr, ""))
		return tr
	}

	// Parse input once (best-effort; used by permission check + hook payloads).
	input, inputErr := tc.ParseInput()

	if eng != nil {
		if inputErr != nil {
			tr := errorResult(tc, fmt.Sprintf("parse input: %v", inputErr))
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
		case permission.BehaviorDeny:
			tr := deniedResult(tc, decision.Message)
			emit(toolResultEvent(tc, tr, ""))
			return tr
		case permission.BehaviorAsk:
			if !interactive {
				tr := deniedResult(tc, decision.Message)
				emit(toolResultEvent(tc, tr, ""))
				return tr
			}
			emit(event.RequireApproval{ID: tc.ID, ToolName: tc.Name, Input: tc.Input})
			d := "deny"
			select {
			case d = <-approvalCh:
			case <-ctx.Done():
			}
			if d == "always" {
				eng.AddRule(permission.Rule{ToolName: tc.Name, Behavior: permission.BehaviorAllow, Source: "user"})
			}
			if d != "allow" && d != "always" {
				tr := deniedResult(tc, "denied by user")
				emit(toolResultEvent(tc, tr, ""))
				return tr
			}
			// allow/always → fall through to execute
		}
	}

	// M4c: PreToolUse hook (after permission, before execute). Block → deny.
	if r, _ := runner.Run(ctx, "PreToolUse", map[string]any{"tool_name": tc.Name, "tool_input": input}); r.Block {
		tr := deniedResult(tc, "blocked by hook: "+r.Reason)
		emit(toolResultEvent(tc, tr, ""))
		return tr
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
	// M4c: PostToolUse hook (inject context into output before emit).
	if r, _ := runner.Run(ctx, "PostToolUse", map[string]any{"tool_name": tc.Name, "tool_input": input, "tool_output": out}); r.Context != "" {
		tr.Output = out + "\n\n" + r.Context
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
