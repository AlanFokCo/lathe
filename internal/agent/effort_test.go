package agent

import (
	"context"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// TestApplyEffortAppendsOption — M7b: a non-empty level flows into every
// Chat/ChatStream as model.WithReasoningEffort.
func TestApplyEffortAppendsOption(t *testing.T) {
	rec := &optRecorder{}
	cm := applyEffort(rec, "high")
	_, _ = cm.Chat(context.Background(), nil)
	o := applied(rec.lastOpts)
	if o.ReasoningEffort == nil || *o.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %+v, want high", o.ReasoningEffort)
	}
	_, _ = cm.ChatStream(context.Background(), nil)
	o2 := applied(rec.lastOpts)
	if o2.ReasoningEffort == nil || *o2.ReasoningEffort != "high" {
		t.Fatal("ChatStream missing ReasoningEffort")
	}
}

func TestApplyEffortEmptyPassthrough(t *testing.T) {
	rec := &optRecorder{}
	cm := applyEffort(rec, "")
	_, _ = cm.Chat(context.Background(), nil)
	if applied(rec.lastOpts).ReasoningEffort != nil {
		t.Fatal("ReasoningEffort should be nil when level empty")
	}
}

func TestApplyEffortSetToggle(t *testing.T) {
	rec := &optRecorder{}
	ef := newEfforter(rec, "")
	cm := model.ChatModel(ef)
	_, _ = cm.Chat(context.Background(), nil)
	if applied(rec.lastOpts).ReasoningEffort != nil {
		t.Fatal("should start empty")
	}
	ef.SetEffort("medium")
	_, _ = cm.Chat(context.Background(), nil)
	if o := applied(rec.lastOpts); o.ReasoningEffort == nil || *o.ReasoningEffort != "medium" {
		t.Fatalf("SetEffort not applied: %+v", o.ReasoningEffort)
	}
	if ef.Effort() != "medium" {
		t.Fatalf("Effort() = %q", ef.Effort())
	}
}
