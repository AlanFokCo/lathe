package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/subagent"
)

// extractText pulls the concatenated text out of a ToolResponse's Content blocks.
func extractText(resp *tool.ToolResponse) string {
	got := ""
	for _, b := range resp.Content {
		if tb, ok := b.(message.TextBlock); ok {
			got += tb.Text
		}
	}
	return got
}

func TestTaskToolReturnsSubagentText(t *testing.T) {
	subModel := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("subagent result"), finalChunk(&model.ChatUsage{})},
	}}
	tt := NewTaskTool(subModel, bypassEngine(), 10, tool.NewToolkit())
	resp, err := tt.Execute(context.Background(), map[string]any{"description": "d", "prompt": "do X"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); !strings.Contains(got, "subagent result") {
		t.Fatalf("Task output: %q", got)
	}
}

func TestTaskToolRunsSubagentToolCall(t *testing.T) {
	// turn 1: subagent calls echo; turn 2: subagent emits final text "done".
	subModel := &fakeModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "echo", `{"msg":"hi"}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	subTk := tool.NewToolkit(echoTool()) // echo is the only tool the subagent can call
	tt := NewTaskTool(subModel, bypassEngine(), 10, subTk)
	resp, err := tt.Execute(context.Background(), map[string]any{"description": "d", "prompt": "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	// Task returns the subagent's TextDelta (final text), not the echo ToolResult.
	if got := extractText(resp); !strings.Contains(got, "done") {
		t.Fatalf("Task output: %q", got)
	}
}

func TestTaskToolCheckPermissionsAllow(t *testing.T) {
	tt := NewTaskTool(&fakeModel{}, bypassEngine(), 10, tool.NewToolkit())
	d := tt.CheckPermissions(nil, nil)
	if d.Behavior != permission.BehaviorAllow {
		t.Fatalf("want allow, got %v", d.Behavior)
	}
}

// TestTaskToolRecordsInTracker — M7e: when a tracker is supplied, Task
// records the subagent's lifecycle (started running, then completed with
// output length + duration) so /agents can show what the parent has
// dispatched without dumping the subagent's full transcript.
func TestTaskToolRecordsInTracker(t *testing.T) {
	subModel := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	tr := subagent.NewTracker()
	tt := NewTaskToolWithTracker(subModel, bypassEngine(), 10, tool.NewToolkit(), tr)
	_, err := tt.Execute(context.Background(), map[string]any{"description": "sweep files", "prompt": "do it"})
	if err != nil {
		t.Fatal(err)
	}
	agents := tr.List()
	if len(agents) != 1 {
		t.Fatalf("agents: %+v", agents)
	}
	a := agents[0]
	if a.Description != "sweep files" || a.Status != "completed" {
		t.Fatalf("agent record: %+v", a)
	}
	if a.OutputBytes == 0 {
		t.Fatalf("OutputBytes should be > 0 after Execute: %+v", a)
	}
	if a.Duration <= 0 {
		t.Fatalf("Duration should be positive: %+v", a)
	}
}

func TestTaskToolMissingPrompt(t *testing.T) {
	tt := NewTaskTool(&fakeModel{}, bypassEngine(), 10, tool.NewToolkit())
	resp, err := tt.Execute(context.Background(), map[string]any{"description": "d"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); !strings.Contains(got, "prompt is required") {
		t.Fatalf("want error about missing prompt, got %q", got)
	}
}
