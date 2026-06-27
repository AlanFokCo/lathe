package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

func TestFormatSummary(t *testing.T) {
	template := "T:{task_overview} C:{current_state}"
	result := json.RawMessage(`{"task_overview":"do X","current_state":"done Y"}`)
	got, err := formatSummary(template, result)
	if err != nil {
		t.Fatal(err)
	}
	if got != "T:do X C:done Y" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildCompressionMessages(t *testing.T) {
	toCompress := []*message.Msg{message.UserMsg("u", "hello")}
	msgs := buildCompressionMessages("SYS", "", toCompress, "PROMPT")
	if len(msgs) != 3 {
		t.Fatalf("len: %d", len(msgs))
	}
	if msgs[0].Role != message.RoleSystem || msgs[1].Role != message.RoleUser || msgs[2].Role != message.RoleUser {
		t.Fatalf("roles: %v %v %v", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	if sys := msgs[0].GetTextContent(" "); sys == nil || !strings.Contains(*sys, "SYS") {
		t.Fatalf("system msg: %v", msgs[0])
	}
}

// fakeCounter returns len(msgs)*100 so the split is deterministic and budget-driven.
type fakeCounter struct{}

func (f *fakeCounter) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return len(msgs) * 100
}

func TestSplitContextReservesRecent(t *testing.T) {
	conv := []*message.Msg{
		message.SystemMsg("s", "SYS"),
		message.UserMsg("u", "old1"),
		message.UserMsg("u", "old2"),
		message.UserMsg("u", "recent1"),
		message.UserMsg("u", "recent2"),
	}
	toCompress, toReserve := splitContextForCompression(conv, 250, &fakeCounter{}, nil)
	if len(toCompress)+len(toReserve) != 4 {
		t.Fatalf("partition sizes: compress=%d reserve=%d", len(toCompress), len(toReserve))
	}
	if len(toReserve) == 0 {
		t.Fatal("expected some reserved messages")
	}
	last := toReserve[len(toReserve)-1].GetTextContent(" ")
	if last == nil || *last != "recent2" {
		t.Fatalf("last reserved: %v", last)
	}
}

func TestSplitContextToolPairPreservation(t *testing.T) {
	conv := []*message.Msg{
		message.SystemMsg("s", "SYS"),
		message.UserMsg("u", "ask"),
		message.AssistantMsg("a", []message.ContentBlock{message.ToolCallBlock{Type: "tool_call", ID: "t1", Name: "R"}}),
		message.AssistantMsg("a", []message.ContentBlock{message.ToolResultBlock{Type: "tool_result", ID: "t1", Name: "R", Output: "res"}}),
		message.UserMsg("u", "recent"),
	}
	toCompress, toReserve := splitContextForCompression(conv, 350, &fakeCounter{}, nil)
	resultInCompress := false
	for _, m := range toCompress {
		if len(m.GetContentBlocks(message.ContentBlockToolResult)) > 0 {
			resultInCompress = true
		}
	}
	if !resultInCompress {
		t.Fatal("expected tool_result in toCompress (pushed with its call)")
	}
	for _, m := range toReserve {
		if len(m.GetContentBlocks(message.ContentBlockToolResult)) > 0 {
			t.Fatal("tool_result orphaned in toReserve")
		}
	}
}

// compressFakeModel scripts CountTokens (fixed) and Chat (returns a structured-output
// tool call so model.GenerateStructuredOutput can extract it).
type compressFakeModel struct{ tokenCount int }

func (m *compressFakeModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	summaryJSON := `{"task_overview":"TO","current_state":"CS","important_discoveries":"ID","next_steps":"NS","context_to_preserve":"CP"}`
	return &model.ChatResponse{
		Content: []message.ContentBlock{message.ToolCallBlock{
			Type: "tool_call", ID: "g1", Name: "generate_structured_output", Input: summaryJSON,
		}},
	}, nil
}
func (m *compressFakeModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, model.ErrStreamNotSupported
}
func (m *compressFakeModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return m.tokenCount
}

func TestCompressContextNoopUnderThreshold(t *testing.T) {
	m := &compressFakeModel{tokenCount: 100}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	// ResolveContextSize falls back to 128000 (fake has no ContextSize/ModelName);
	// threshold = 128000*0.8 = 102400; 100 < threshold → no-op.
	compacted, before, after, err := eng.compressContext(context.Background(), false)
	if err != nil || compacted {
		t.Fatalf("expected no-op: compacted=%v err=%v", compacted, err)
	}
	if before != 100 || after != 0 {
		t.Fatalf("before=%d after=%d", before, after)
	}
}

func TestCompressContextOverThreshold(t *testing.T) {
	m := &compressFakeModel{tokenCount: 200000}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	eng.conv = append(eng.conv,
		message.UserMsg("u", "old1"),
		message.UserMsg("u", "old2"),
		message.UserMsg("u", "recent"),
	)
	compacted, before, _, err := eng.compressContext(context.Background(), false)
	if err != nil || !compacted {
		t.Fatalf("expected compact: compacted=%v err=%v", compacted, err)
	}
	if before != 200000 {
		t.Fatalf("before=%d", before)
	}
	if len(eng.conv) < 2 {
		t.Fatalf("conv too short: %d", len(eng.conv))
	}
	if eng.conv[1].Role != message.RoleUser {
		t.Fatalf("summary msg role: %v", eng.conv[1].Role)
	}
	if s := eng.conv[1].GetTextContent(" "); s == nil || !strings.Contains(*s, "Previous context summary") {
		t.Fatalf("summary msg: %v", eng.conv[1])
	}
}
