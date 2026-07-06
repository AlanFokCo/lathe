package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// fakeModel implements model.ChatModel for tests. The i-th Chat call returns
// the chunks in turns[i] merged into a single *ChatResponse (M6a: UnifiedAgent
// calls sync Chat, not ChatStream; the old streaming fakes are replaced by a
// sync model that accumulates a turn's chunks into one response).
type fakeModel struct {
	turns [][]model.ChatResponse
	calls int
}

func (f *fakeModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	if f.calls >= len(f.turns) {
		return nil, errors.New("fakeModel: no more scripted turns")
	}
	chunks := f.turns[f.calls]
	f.calls++
	return mergeChunks(chunks), nil
}

// ChatStream is unused under v3 (UnifiedAgent drives sync Chat). It returns an
// error so any stray streaming call surfaces loudly instead of hanging.
func (f *fakeModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, errors.New("fakeModel: ChatStream not used under v3")
}

func (f *fakeModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int { return 0 }

// mergeChunks folds a turn's streamed ChatResponse chunks into one sync
// response: concatenated text deltas, tool calls merged by ID (Input appended,
// so both full-block and partial-JSON-delta streaming work), and usage taken
// from the IsLast chunk. Mirrors what a real provider returns in one Chat call.
func mergeChunks(chunks []model.ChatResponse) *model.ChatResponse {
	var sb strings.Builder
	byID := map[string]message.ToolCallBlock{}
	var order []string
	var usage *model.ChatUsage
	for _, c := range chunks {
		if dt := c.GetTextContent(); dt != "" {
			sb.WriteString(dt)
		}
		for _, b := range c.Content {
			if tc, ok := b.(message.ToolCallBlock); ok {
				if existing, seen := byID[tc.ID]; seen {
					existing.Input += tc.Input
					byID[tc.ID] = existing
				} else {
					byID[tc.ID] = tc
					order = append(order, tc.ID)
				}
			}
		}
		if c.IsLast && c.Usage != nil {
			usage = c.Usage
		}
	}
	var blocks []message.ContentBlock
	if dt := sb.String(); dt != "" {
		blocks = append(blocks, message.TextBlock{Type: "text", Text: dt})
	}
	for _, id := range order {
		blocks = append(blocks, byID[id])
	}
	return &model.ChatResponse{Content: blocks, IsLast: true, Usage: usage}
}

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

func TestFakeModelChatMergesChunks(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("Hel"), textChunk("lo"), finalChunk(&model.ChatUsage{InputTokens: 3, OutputTokens: 2})},
	}}
	resp, err := m.Chat(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetTextContent(); got != "Hello" {
		t.Fatalf("text: got %q", got)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 3 {
		t.Fatalf("usage: %v", resp.Usage)
	}
}
