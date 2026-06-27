package agent

import (
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/lathe/internal/event"
)

// accumulate consumes a ChatStream channel and merges it into:
//   - full text: concatenated text deltas from non-last chunks
//   - tool calls: collected from all chunks, merged by ID (Input appended,
//     so both full-block and partial-JSON-delta streaming work)
//   - usage: from the IsLast chunk
//   - text deltas: one event per non-last text fragment
//
// Pattern follows examples/model_call/main.go: non-last chunks carry text
// deltas; the IsLast chunk carries Usage.
func accumulate(ch <-chan model.ChatResponse) (text string, toolCalls []message.ToolCallBlock, usage *model.ChatUsage, deltas []event.TextDelta) {
	var sb strings.Builder
	byID := map[string]message.ToolCallBlock{}
	var order []string
	for resp := range ch {
		if !resp.IsLast {
			dt := resp.GetTextContent()
			if dt != "" {
				sb.WriteString(dt)
				deltas = append(deltas, event.TextDelta{Delta: dt})
			}
		}
		for _, b := range resp.Content {
			if tc, ok := b.(message.ToolCallBlock); ok {
				if existing, seen := byID[tc.ID]; seen {
					existing.Input += tc.Input
					byID[tc.ID] = existing
				} else {
					byID[tc.ID] = tc
					order = append(order, tc.ID)
				}
			}
		}
		if resp.IsLast && resp.Usage != nil {
			usage = resp.Usage
		}
	}
	for _, id := range order {
		toolCalls = append(toolCalls, byID[id])
	}
	return sb.String(), toolCalls, usage, deltas
}
