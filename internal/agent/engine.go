package agent

import (
	"context"

	asevent "github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
)

// Run executes the agent for a single user prompt, streaming agentscope block-
// lifecycle events. M6a Commit B: Run is a thin hook wrapper over UnifiedAgent
// .ReplyStream — it runs the UserPromptSubmit/Stop settings hooks and captures
// the pending HITL tool call (for SubmitApproval), then passes events through
// UNCHANGED so render/tui consume agentscope events directly (no translator,
// no internal/event).
func (e *Engine) Run(ctx context.Context, prompt string) <-chan asevent.Event {
	ch := make(chan asevent.Event, 64)
	go e.runWrap(ctx, prompt, ch)
	return ch
}

// runWrap runs the UserPromptSubmit hook, drives ReplyStream, and pumps
// agentscope events to ch. It side-effects on RequireUserConfirmEvent (records
// the pending approval) and ReplyEndEvent (fires the Stop hook); all events
// are passed through unchanged.
func (e *Engine) runWrap(ctx context.Context, prompt string, ch chan<- asevent.Event) {
	defer close(ch)
	// M4c: UserPromptSubmit hook (inject context into the prompt) — run here so
	// the injected context reaches the model on the first call.
	if e.hookRunner != nil {
		if r, _ := e.hookRunner.Run(ctx, "UserPromptSubmit", map[string]any{"prompt": prompt}); r.Context != "" {
			prompt = prompt + "\n\n" + r.Context
		}
	}
	asCh, err := e.agent.ReplyStream(ctx, prompt)
	if err != nil {
		// ReplyStream only fails on empty input (validated paths ensure non-empty).
		// Surface as a custom error event so consumers see something + a stream end.
		emitAsevent(ctx, ch, asevent.NewCustomEvent("", "error", map[string]any{"error": err.Error()}))
		return
	}
	var sawExceed, stopFired, sawReplyEnd bool
	for asEv := range asCh {
		switch ev := asEv.(type) {
		case asevent.ExceedMaxItersEvent:
			sawExceed = true
		case asevent.RequireUserConfirmEvent:
			if len(ev.ToolCalls) > 0 {
				e.setPending(&pendingApproval{replyID: ev.ReplyID, toolCall: ev.ToolCalls[0]})
			}
		case asevent.ReplyEndEvent:
			sawReplyEnd = true
			// M4c: fire the Stop hook before passing ReplyEnd through (matches the
			// old finishTurn ordering: Stop then ReplyEnd).
			if !stopFired {
				stopFired = true
				e.fireStop(ctx, endTurnReason(sawExceed))
			}
		}
		emitAsevent(ctx, ch, asEv)
	}
	// Defensive: if the stream ended without a ReplyEndEvent (e.g. agentscope's
	// ctx-aware emit dropped it on cancel), fire Stop + emit a synthetic
	// ReplyEnd so consumers always see a clean turn end.
	if !stopFired {
		stopFired = true
		e.fireStop(ctx, endTurnReason(sawExceed))
	}
	if !sawReplyEnd {
		emitAsevent(ctx, ch, asevent.NewReplyEndEvent(e.state.SessionID, e.state.ReplyID))
	}
}

func endTurnReason(sawExceed bool) string {
	if sawExceed {
		return "max_iters"
	}
	return "end_turn"
}

// pendingApproval is the HITL bridge state: runWrap records the pending
// RequireUserConfirm (replyID + tool call) and SubmitApproval consumes it to
// build a UserConfirmResultEvent for agent.SubmitUserConfirm.
type pendingApproval struct {
	replyID  string
	toolCall message.ToolCallBlock
}

func (e *Engine) setPending(p *pendingApproval) {
	e.pendingMu.Lock()
	e.pending = p
	e.pendingMu.Unlock()
}

func (e *Engine) getPending() *pendingApproval {
	e.pendingMu.Lock()
	defer e.pendingMu.Unlock()
	return e.pending
}

func (e *Engine) clearPending() {
	e.pendingMu.Lock()
	e.pending = nil
	e.pendingMu.Unlock()
}

// fireStop runs the Stop hook (fire-and-forget). Called on ReplyEnd.
func (e *Engine) fireStop(ctx context.Context, reason string) {
	if e.hookRunner != nil {
		e.hookRunner.Run(ctx, "Stop", map[string]any{"reason": reason})
	}
}

// SetInteractive enables (TUI) or disables (print) interactive approval. M6a:
// maps the configured permission mode to an effective mode — print (non-
// interactive) collapses default/accept_edits to dont_ask so an Ask cannot
// hang waiting for a user who is not there (matches the old engine's
// !interactive→deny path); TUI restores the configured mode so default/
// accept_edits surface RequireUserConfirm for the TUI to approve.
func (e *Engine) SetInteractive(interactive bool) {
	if e.permEng == nil || e.permEng.Context == nil {
		return
	}
	e.permEng.Context.Mode = e.effectiveMode(interactive)
}

func (e *Engine) effectiveMode(interactive bool) permission.PermissionMode {
	if interactive {
		return e.configuredMode
	}
	return printEffectiveMode(e.configuredMode)
}

// printEffectiveMode maps a configured permission mode to its non-interactive
// (print) effective mode. default/accept_edits → dont_ask (an Ask would hang
// with no user to confirm). bypass/explore/dont_ask never Ask, so unchanged.
func printEffectiveMode(m permission.PermissionMode) permission.PermissionMode {
	switch m {
	case permission.ModeDefault, permission.ModeAcceptEdits:
		return permission.ModeDontAsk
	default:
		return m
	}
}

// SubmitApproval delivers the user's approval decision ("allow"/"deny"/"always")
// to the agent's pending RequireUserConfirm. M6a: bridges to agentscope's
// SubmitUserConfirm. "always" attaches an allow Rule to the ConfirmResult;
// agentscope's waitForConfirmation AddRule's it to the shared permission
// Context (lathe's permEng sees it too — same Context pointer).
func (e *Engine) SubmitApproval(decision string) {
	pending := e.getPending()
	if pending == nil || e.agent == nil {
		return
	}
	var rules []any
	if decision == "always" {
		rules = []any{permission.Rule{
			ToolName: pending.toolCall.Name,
			Behavior: permission.BehaviorAllow,
			Source:   "user",
		}}
	}
	approved := decision == "allow" || decision == "always"
	result := asevent.NewUserConfirmResultEvent(pending.replyID, []asevent.ConfirmResult{{
		Confirmed: approved,
		ToolCall:  pending.toolCall,
		Rules:     rules,
	}})
	e.agent.SubmitUserConfirm(&result)
	e.clearPending()
}

func emitAsevent(ctx context.Context, ch chan<- asevent.Event, ev asevent.Event) {
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
