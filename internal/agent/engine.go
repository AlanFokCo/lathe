package agent

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/lathe/internal/event"
)

// Run executes the turn loop for a single user prompt, streaming events.
// Loop: ChatStream → accumulate → emit text/usage → if tool calls, dispatch
// and feed results back as an assistant-role message → repeat until no tool
// calls or MaxIters.
func (e *Engine) Run(ctx context.Context, prompt string) <-chan event.Event {
	ch := make(chan event.Event, 64)
	go e.runLoop(ctx, prompt, ch)
	return ch
}

func (e *Engine) runLoop(ctx context.Context, prompt string, ch chan<- event.Event) {
	defer close(ch)
	e.conv = append(e.conv, message.UserMsg(e.name, prompt))
	tools := e.toolkit.GetToolSchemas()

	for iter := 0; iter < e.maxIters; iter++ {
		if ctx.Err() != nil {
			emitEvent(ctx, ch, event.ReplyEnd{Reason: "cancelled"})
			return
		}
		chunkCh, err := e.chatModel.ChatStream(ctx, e.conv,
			model.WithTools(tools),
			model.WithToolChoice(&model.ToolChoice{Mode: "auto"}),
		)
		if err != nil {
			emitEvent(ctx, ch, event.ErrorEvent{Err: err})
			emitEvent(ctx, ch, event.ReplyEnd{Reason: "error"})
			return
		}
		text, toolCalls, usage, deltas := accumulate(chunkCh)
		for _, d := range deltas {
			emitEvent(ctx, ch, d)
		}
		if usage != nil {
			modelName := ""
			if mn, ok := e.chatModel.(interface{ ModelName() string }); ok {
				modelName = mn.ModelName()
			}
			emitEvent(ctx, ch, event.Usage{
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, Model: modelName,
			})
		}

		// assistant message with text + tool calls
		var blocks []message.ContentBlock
		if text != "" {
			blocks = append(blocks, message.TextBlock{Type: "text", Text: text})
		}
		blocks = append(blocks, toolCallsToBlocks(toolCalls)...)
		e.conv = append(e.conv, message.AssistantMsg(e.name, blocks))

		if len(toolCalls) == 0 {
			emitEvent(ctx, ch, event.ReplyEnd{Reason: "end_turn"})
			return
		}

		results := dispatch(ctx, toolCalls, e.toolkit, e.permEng,
			func(ev event.Event) { emitEvent(ctx, ch, ev) })
		// tool results go in an assistant-role message (agentscope-go convention;
		// formatters translate to each provider's wire format).
		e.conv = append(e.conv, message.AssistantMsg(e.name, toolResultsToBlocks(results)))
	}
	emitEvent(ctx, ch, event.ReplyEnd{Reason: "max_iters"})
}

func toolCallsToBlocks(calls []message.ToolCallBlock) []message.ContentBlock {
	out := make([]message.ContentBlock, len(calls))
	for i, c := range calls {
		out[i] = c
	}
	return out
}

func toolResultsToBlocks(results []message.ToolResultBlock) []message.ContentBlock {
	out := make([]message.ContentBlock, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out
}

func emitEvent(ctx context.Context, ch chan<- event.Event, ev event.Event) {
	// Prefer a non-blocking send (the channel is buffered); only fall back to a
	// ctx-aware send when the buffer is full. This ensures terminal events
	// (e.g. ReplyEnd on cancel) are still emitted when ctx is already done.
	select {
	case ch <- ev:
	default:
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}
}
