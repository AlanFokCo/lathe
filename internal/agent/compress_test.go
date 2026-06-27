package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
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
	// reserveBudget=250: [sys,recent2]=200<250; [sys,recent1,recent2]=300>=250 → splitIdx=3
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
	// conv: system, user, assistant(tool_call t1), assistant(tool_result t1), user(recent)
	conv := []*message.Msg{
		message.SystemMsg("s", "SYS"),
		message.UserMsg("u", "ask"),
		message.AssistantMsg("a", []message.ContentBlock{message.ToolCallBlock{Type: "tool_call", ID: "t1", Name: "R"}}),
		message.AssistantMsg("a", []message.ContentBlock{message.ToolResultBlock{Type: "tool_result", ID: "t1", Name: "R", Output: "res"}}),
		message.UserMsg("u", "recent"),
	}
	// reserveBudget=350: walk → [sys,call,result,recent]=400>=350 at i=1 → splitIdx=2 (between call and result)
	// then adjustSplitForToolPairs pushes result (orphan, its call is in compress) into compress → splitIdx=3
	toCompress, toReserve := splitContextForCompression(conv, 350, &fakeCounter{}, nil)

	// the tool_result must end up in toCompress (pushed with its call), not orphaned in toReserve.
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
