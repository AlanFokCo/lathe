package agent

import (
	"context"
	"sync"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// efforter wraps a ChatModel so every Chat / ChatStream picks up
// model.WithReasoningEffort(level) when a non-empty level is set (M7b).
// Layered outside thinker + resilience so retries preserve the option and
// SetModel preserves the live level. Only OpenAI's reasoning models honor it;
// providers that ignore the option are unaffected.
type efforter struct {
	inner model.ChatModel
	mu    sync.RWMutex
	level string
}

func newEfforter(inner model.ChatModel, level string) *efforter {
	return &efforter{inner: inner, level: level}
}

// applyEffort exposes the wrapper as a plain ChatModel for the initial-config
// path.
func applyEffort(cm model.ChatModel, level string) model.ChatModel {
	return newEfforter(cm, level)
}

// SetEffort updates the reasoning effort level; empty disables it.
func (e *efforter) SetEffort(level string) {
	e.mu.Lock()
	e.level = level
	e.mu.Unlock()
}

// Effort returns the current level ("" when off).
func (e *efforter) Effort() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.level
}

func (e *efforter) opts(existing []model.CallOption) []model.CallOption {
	e.mu.RLock()
	lvl := e.level
	e.mu.RUnlock()
	if lvl == "" {
		return existing
	}
	return append(existing, model.WithReasoningEffort(lvl))
}

func (e *efforter) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	return e.inner.Chat(ctx, msgs, e.opts(opts)...)
}

func (e *efforter) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return e.inner.ChatStream(ctx, msgs, e.opts(opts)...)
}

func (e *efforter) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return e.inner.CountTokens(msgs, tools)
}
