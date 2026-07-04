package agent

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

// toolResultRoleMiddleware is a lathe product-layer adapter (M6a) that rewrites
// the model-call message list so tool_result blocks live in user-role messages.
//
// agentscope's saveToContext merges a tool's result block into the assistant
// message that issued the tool_call (its convention), and the AnthropicFormatter
// only rewrites role "tool"→"user" (NOT "assistant"→"user"). With the Anthropic
// provider that leaves tool_result on an assistant turn, which the model treats
// as invisible — causing the "stdout wasn't returned to me" tool loop (see
// lathe-run-recipe memory, commit 9ac548b). lathe previously avoided this by
// appending tool results in a user-role message; this middleware preserves that
// invariant under v3 delegation. The clean fix is upstream in agentscope's
// formatter (M6b); this adapter is removed then.
//
// Provider-agnostic: splitting tool_result into its own user message is valid
// for Anthropic (tool_result on a user turn) and for OpenAI/DashScope (whose
// formatters re-role a user message containing tool_result to "tool").
type toolResultRoleMiddleware struct {
	middleware.BaseMiddleware
}

func (m *toolResultRoleMiddleware) OnModelCall(ctx context.Context, input *middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
	input.Messages = splitToolResultsToUserRole(input.Messages)
	return next(ctx, input)
}

// splitToolResultsToUserRole rewrites msgs so any tool_result blocks in an
// assistant message move into a following user-role message; non-result blocks
// (text/thinking/tool_call) stay in the assistant message. System and user
// messages pass through unchanged.
func splitToolResultsToUserRole(msgs []*message.Msg) []*message.Msg {
	out := make([]*message.Msg, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role != message.RoleAssistant {
			out = append(out, m)
			continue
		}
		results := m.GetContentBlocks(message.ContentBlockToolResult)
		if len(results) == 0 {
			out = append(out, m)
			continue
		}
		var keep []message.ContentBlock
		for _, b := range m.GetContentBlocks() {
			if _, ok := b.(message.ToolResultBlock); !ok {
				keep = append(keep, b)
			}
		}
		if len(keep) > 0 {
			split := *m
			split.Content = keep
			out = append(out, &split)
		}
		out = append(out, message.NewMsg(m.Name, message.RoleUser, results))
	}
	return out
}
