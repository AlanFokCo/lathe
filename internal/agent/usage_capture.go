package agent

import (
	"context"
	"sync"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

// usageCapturingModel wraps a model.ChatModel and stashes the last ChatUsage
// seen from a Chat call. The v3 UnifiedAgent calls Chat (sync) and emits a
// ModelCallEndEvent that carries only input/output tokens; lathe's Usage event
// also surfaces prompt-cache tokens (M6a hardening). The translator reads the
// stashed usage to preserve cache-token visibility.
type usageCapturingModel struct {
	inner model.ChatModel
	mu    sync.Mutex
	last  *model.ChatUsage
}

// wrapUsageCapture wraps inner so the engine can recover full usage (incl.
// cache tokens) per model call.
func wrapUsageCapture(inner model.ChatModel) *usageCapturingModel {
	return &usageCapturingModel{inner: inner}
}

func (u *usageCapturingModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	resp, err := u.inner.Chat(ctx, msgs, opts...)
	if err == nil && resp != nil && resp.Usage != nil {
		u.mu.Lock()
		u.last = resp.Usage
		u.mu.Unlock()
	}
	return resp, err
}

func (u *usageCapturingModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return u.inner.ChatStream(ctx, msgs, opts...)
}

func (u *usageCapturingModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return u.inner.CountTokens(msgs, tools)
}

// LastUsage returns the most recently observed ChatUsage (nil until the first
// successful Chat). Safe for concurrent use.
func (u *usageCapturingModel) LastUsage() *model.ChatUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}
