package agent

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
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
	// M4c: UserPromptSubmit hook (inject context into the prompt).
	if r, _ := e.hookRunner.Run(ctx, "UserPromptSubmit", map[string]any{"prompt": prompt}); r.Context != "" {
		prompt = prompt + "\n\n" + r.Context
	}
	e.appendConv(message.UserMsg(e.name, prompt))
	tools := e.toolkit.GetToolSchemas()

	// M6a: inject the ReadCache so the base Write/Edit read-before-write guard is
	// active for this turn (host mode; sandbox tools are bare closures unaffected).
	if e.readCache != nil {
		ctx = tool.WithReadCache(ctx, e.readCache)
	}

	for iter := 0; iter < e.maxIters; iter++ {
		if ctx.Err() != nil {
			emitEvent(ctx, ch, event.ReplyEnd{Reason: "cancelled"})
			return
		}
		emitEvent(ctx, ch, event.TurnStep{Iter: iter + 1, MaxIters: e.maxIters})
		// auto-compact if the conversation exceeds the context threshold
		if compacted, before, after, cerr := e.compressContext(ctx, false); cerr != nil {
			emitEvent(ctx, ch, event.ErrorEvent{Err: cerr})
		} else if compacted {
			emitEvent(ctx, ch, event.Compacted{Before: before, After: after})
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
		text, toolCalls, usage := accumulate(chunkCh, func(ev event.Event) { emitEvent(ctx, ch, ev) })
		if usage != nil {
			// M6a: source the model name from config (the resilience wrapper hides
			// the provider's ModelName()); cfg.Model is authoritative.
			modelName := ""
			if e.cfg != nil {
				modelName = e.cfg.Model
			}
			emitEvent(ctx, ch, event.Usage{
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheCreationTokens: usage.CacheCreationInputTokens,
				CacheReadTokens:     usage.CacheInputTokens,
				Model:               modelName,
			})
		}

		// assistant message with text + tool calls
		var blocks []message.ContentBlock
		if text != "" {
			blocks = append(blocks, message.TextBlock{Type: "text", Text: text})
		}
		blocks = append(blocks, toolCallsToBlocks(toolCalls)...)
		e.appendConv(message.AssistantMsg(e.name, blocks))

		if len(toolCalls) == 0 {
			e.finishTurn(ctx, ch, "end_turn")
			return
		}

		results := dispatch(ctx, toolCalls, e.toolkit, e.permEng, e.interactive, e.approvalCh,
			func(ev event.Event) { emitEvent(ctx, ch, ev) }, e.hookRunner)
		// M6a: truncate large tool results before they enter the conversation. The
		// full output already went out in the ToolResult event (for the TUI); this
		// only bounds what is persisted into e.conv, preventing one huge result
		// from blowing the context window before auto-compact can react.
		for i := range results {
			if s, ok := results[i].Output.(string); ok {
				if truncated, did := truncateToolResult(s, e.compressCfg.ToolResultLimit); did {
					results[i].Output = truncated
				}
			}
		}
		// tool results go in a USER-role message. Anthropic requires tool_result
		// blocks in a user message (an assistant-role tool_result is invisible to
		// the model → it loops, "stdout wasn't returned"). OpenAI/DashScope
		// formatters override the role to "tool" when a ToolResultBlock is present,
		// so they are unaffected by this role choice.
		e.appendConv(message.NewMsg(e.name, message.RoleUser, toolResultsToBlocks(results)))
	}
	e.finishTurn(ctx, ch, "max_iters")
}

// finishTurn fires the Stop hook (fire-and-forget) then emits ReplyEnd.
// cancelled/error exits do not fire Stop (interrupted/exceptional paths).
func (e *Engine) finishTurn(ctx context.Context, ch chan<- event.Event, reason string) {
	e.hookRunner.Run(ctx, "Stop", map[string]any{"reason": reason})
	emitEvent(ctx, ch, event.ReplyEnd{Reason: reason})
}

// appendConv appends a message to e.conv and persists it to the session (if any).
func (e *Engine) appendConv(msg *message.Msg) {
	e.conv = append(e.conv, msg)
	if e.session != nil {
		_ = e.session.Save(msg)
	}
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

// truncateToolResult bounds a tool result to ~tokenLimit tokens (estimated as
// len/4) before it enters the conversation, appending a marker when it cuts.
// Reimplemented locally (mirrors agentscope's agent.TruncateToolResult) to avoid
// importing the base agent package, which pulls in heavy transitive deps
// (otel, qdrant) lathe otherwise doesn't need.
func truncateToolResult(text string, tokenLimit int) (string, bool) {
	if len(text)/4 <= tokenLimit {
		return text, false
	}
	charLimit := tokenLimit * 4
	if charLimit >= len(text) {
		return text, false
	}
	return text[:charLimit] + "\n<<<TRUNCATED>>>", true
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
