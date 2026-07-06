package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// optRecorder captures the last CallOptions passed to Chat/ChatStream so
// tests can assert what a wrapper appended. It returns an empty stream so the
// happy-path exercises the wrapping without touching the network.
type optRecorder struct {
	mu        sync.Mutex
	callCount int
	lastOpts  []model.CallOption
}

func (r *optRecorder) Chat(_ context.Context, _ []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	r.mu.Lock()
	r.callCount++
	r.lastOpts = append([]model.CallOption(nil), opts...)
	r.mu.Unlock()
	return &model.ChatResponse{}, nil
}
func (r *optRecorder) ChatStream(_ context.Context, _ []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	r.mu.Lock()
	r.callCount++
	r.lastOpts = append([]model.CallOption(nil), opts...)
	r.mu.Unlock()
	ch := make(chan model.ChatResponse)
	close(ch)
	return ch, nil
}
func (r *optRecorder) CountTokens(_ []*message.Msg, _ []model.ToolSchema) int { return 0 }

func applied(opts []model.CallOption) model.CallOptions {
	var o model.CallOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// TestApplyThinkingAppendsOption — M7a: when thinking is enabled, every
// Chat/ChatStream picks up WithThinking(true, budget).
func TestApplyThinkingAppendsOption(t *testing.T) {
	rec := &optRecorder{}
	cm := applyThinking(rec, true, 4096)
	_, _ = cm.Chat(context.Background(), nil)
	o := applied(rec.lastOpts)
	if o.ThinkingEnable == nil || !*o.ThinkingEnable {
		t.Fatalf("ThinkingEnable = %+v, want true", o.ThinkingEnable)
	}
	if o.ThinkingBudget == nil || *o.ThinkingBudget != 4096 {
		t.Fatalf("ThinkingBudget = %+v, want 4096", o.ThinkingBudget)
	}
	_, _ = cm.ChatStream(context.Background(), nil)
	o2 := applied(rec.lastOpts)
	if o2.ThinkingEnable == nil || !*o2.ThinkingEnable {
		t.Fatalf("ChatStream missing ThinkingEnable")
	}
}

// TestApplyThinkingDisabledPassthrough — thinking off → opts unchanged (no
// spurious ThinkingEnable flag for providers that would ignore it).
func TestApplyThinkingDisabledPassthrough(t *testing.T) {
	rec := &optRecorder{}
	cm := applyThinking(rec, false, 0)
	_, _ = cm.Chat(context.Background(), nil)
	o := applied(rec.lastOpts)
	if o.ThinkingEnable != nil {
		t.Fatalf("ThinkingEnable should stay nil when disabled: %+v", o.ThinkingEnable)
	}
}

// TestApplyThinkingSetToggle — SetThinking flips live for the /thinking slash
// (so users can turn thinking on mid-session without a restart).
func TestApplyThinkingSetToggle(t *testing.T) {
	rec := &optRecorder{}
	tk := newThinker(rec, false, 0)
	cm := model.ChatModel(tk)
	_, _ = cm.Chat(context.Background(), nil)
	if applied(rec.lastOpts).ThinkingEnable != nil {
		t.Fatal("should start disabled")
	}
	tk.SetThinking(true, 5000)
	_, _ = cm.Chat(context.Background(), nil)
	o := applied(rec.lastOpts)
	if o.ThinkingEnable == nil || !*o.ThinkingEnable || *o.ThinkingBudget != 5000 {
		t.Fatalf("SetThinking did not take effect: %+v %+v", o.ThinkingEnable, o.ThinkingBudget)
	}
	en, bud := tk.Thinking()
	if !en || bud != 5000 {
		t.Fatalf("Thinking() = (%v,%d), want (true,5000)", en, bud)
	}
}
