package agent

import (
	"reflect"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/lathe/internal/event"
)

func TestAccumulateTextDeltasEmittedLive(t *testing.T) {
	ch := make(chan model.ChatResponse, 3)
	ch <- textChunk("Hel")
	ch <- textChunk("lo")
	ch <- finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 2})
	close(ch)

	var emitted []event.TextDelta
	text, tcs, usage := accumulate(ch, func(ev event.Event) {
		if d, ok := ev.(event.TextDelta); ok {
			emitted = append(emitted, d)
		}
	})
	if text != "Hello" {
		t.Fatalf("text: got %q", text)
	}
	if len(tcs) != 0 {
		t.Fatalf("toolCalls: got %d", len(tcs))
	}
	if usage == nil || usage.OutputTokens != 2 {
		t.Fatalf("usage: %v", usage)
	}
	if len(emitted) != 2 || emitted[0].Delta != "Hel" || emitted[1].Delta != "lo" {
		t.Fatalf("emitted deltas (want live, in-order): %+v", emitted)
	}
}

func TestAccumulateToolCallMergedByID(t *testing.T) {
	ch := make(chan model.ChatResponse, 3)
	// tool call split across two deltas (partial JSON), plus final usage
	ch <- model.ChatResponse{Content: []message.ContentBlock{toolCallBlock("t1", "Read", `{"path":"`)}}
	ch <- model.ChatResponse{Content: []message.ContentBlock{toolCallBlock("t1", "Read", `x.txt"}`)}}
	ch <- finalChunk(&model.ChatUsage{})
	close(ch)

	text, tcs, usage := accumulate(ch, func(event.Event) {})
	if text != "" {
		t.Fatalf("text: got %q", text)
	}
	if len(tcs) != 1 {
		t.Fatalf("toolCalls: got %d", len(tcs))
	}
	want := message.ToolCallBlock{Type: "tool_call", ID: "t1", Name: "Read", Input: `{"path":"x.txt"}`}
	if !reflect.DeepEqual(tcs[0], want) {
		t.Fatalf("merged tool call: got %+v want %+v", tcs[0], want)
	}
	if usage == nil {
		t.Fatal("usage nil")
	}
}
