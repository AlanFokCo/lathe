package agent

import (
	"context"
	"sync"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// thinker wraps a ChatModel so every Chat / ChatStream call picks up
// model.WithThinking(true, budget) when thinking is enabled (M7a). Toggled
// live via SetThinking so /thinking on|off|budget=N takes effect without a
// restart. UnifiedAgent hardcodes its CallOptions (Tools + ToolChoice only),
// so wrapping the model is the cleanest place to plumb model-call options in.
type thinker struct {
	inner  model.ChatModel
	mu     sync.RWMutex
	enable bool
	budget int
}

func newThinker(inner model.ChatModel, enable bool, budget int) *thinker {
	return &thinker{inner: inner, enable: enable, budget: budget}
}

// applyThinking returns cm wrapped when caller wants live toggling as well;
// most call sites use it directly for the initial-config path.
func applyThinking(cm model.ChatModel, enable bool, budget int) model.ChatModel {
	return newThinker(cm, enable, budget)
}

// SetThinking updates the enable flag + budget (0 keeps the existing budget so
// `/thinking on` after a previous `/thinking budget=8000` doesn't clobber it).
func (t *thinker) SetThinking(enable bool, budget int) {
	t.mu.Lock()
	t.enable = enable
	if budget > 0 {
		t.budget = budget
	}
	t.mu.Unlock()
}

// Thinking returns the current enable flag + budget.
func (t *thinker) Thinking() (bool, int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.enable, t.budget
}

func (t *thinker) opts(existing []model.CallOption) []model.CallOption {
	t.mu.RLock()
	en, bud := t.enable, t.budget
	t.mu.RUnlock()
	if !en {
		return existing
	}
	return append(existing, model.WithThinking(true, bud))
}

func (t *thinker) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	return t.inner.Chat(ctx, msgs, t.opts(opts)...)
}

func (t *thinker) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return t.inner.ChatStream(ctx, msgs, t.opts(opts)...)
}

func (t *thinker) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return t.inner.CountTokens(msgs, tools)
}
