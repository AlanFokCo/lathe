package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

// fakeModel implements model.ChatModel for tests. The i-th ChatStream call
// emits the chunks in turns[i].
type fakeModel struct {
	turns [][]model.ChatResponse
	calls int
}

func (f *fakeModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	return nil, errors.New("fakeModel: Chat not used")
}

func (f *fakeModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	if f.calls >= len(f.turns) {
		return nil, errors.New("fakeModel: no more scripted turns")
	}
	chunks := f.turns[f.calls]
	f.calls++
	ch := make(chan model.ChatResponse, len(chunks))
	go func() {
		defer close(ch)
		for _, c := range chunks {
			ch <- c
		}
	}()
	return ch, nil
}

func (f *fakeModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int { return 0 }

func textChunk(delta string) model.ChatResponse {
	return model.ChatResponse{
		Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: delta}},
	}
}

func finalChunk(usage *model.ChatUsage, content ...message.ContentBlock) model.ChatResponse {
	return model.ChatResponse{Content: content, IsLast: true, Usage: usage}
}

func toolCallBlock(id, name, input string) message.ToolCallBlock {
	return message.ToolCallBlock{Type: "tool_call", ID: id, Name: name, Input: input}
}

func TestFakeModelStreamsScriptedChunks(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("Hel"), textChunk("lo"), finalChunk(&model.ChatUsage{InputTokens: 3, OutputTokens: 2})},
	}}
	ch, err := m.ChatStream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	var usage *model.ChatUsage
	for r := range ch {
		if !r.IsLast {
			got += r.GetTextContent()
		} else {
			usage = r.Usage
		}
	}
	if got != "Hello" {
		t.Fatalf("text: got %q", got)
	}
	if usage == nil || usage.InputTokens != 3 {
		t.Fatalf("usage: %v", usage)
	}
}
