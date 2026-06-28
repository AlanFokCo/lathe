package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/mcp"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/event"
	"github.com/alanfokco/lathe/internal/hooks"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
)

func drain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func echoToolkit() *tool.Toolkit {
	return tool.NewToolkit(tool.NewFunctionTool("echo", "echo",
		json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			return tool.NewTextResponse("echoed: " + input["msg"].(string)), nil
		},
	))
}

func bypassEngine() *permission.Engine {
	return permission.NewEngine(permission.NewContext(permission.ModeBypass))
}

func TestEnginePureTextTurn(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("Hel"), textChunk("lo"), finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 2})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	evs := drain(eng.Run(context.Background(), "hi"))
	// expect: TextDelta, TextDelta, Usage, ReplyEnd{end_turn}
	if len(evs) != 4 {
		t.Fatalf("events: %+v", evs)
	}
	last := evs[len(evs)-1]
	if re, ok := last.(event.ReplyEnd); !ok || re.Reason != "end_turn" {
		t.Fatalf("last event: %+v", last)
	}
}

func TestEngineSingleToolTurn(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		// turn 1: one tool call
		{finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 1}, toolCallBlock("t1", "echo", `{"msg":"hi"}`))},
		// turn 2: final text
		{textChunk("done"), finalChunk(&model.ChatUsage{InputTokens: 2, OutputTokens: 2})},
	}}
	eng := newEngineForTest(m, echoToolkit(), bypassEngine(), 10)
	evs := drain(eng.Run(context.Background(), "call echo"))
	var sawToolResult, sawEnd bool
	for _, ev := range evs {
		switch e := ev.(type) {
		case event.ToolResult:
			sawToolResult = true
			if e.State != "success" {
				t.Fatalf("tool state: %s", e.State)
			}
		case event.ReplyEnd:
			sawEnd = true
			if e.Reason != "end_turn" {
				t.Fatalf("reason: %s", e.Reason)
			}
		}
	}
	if !sawToolResult || !sawEnd {
		t.Fatalf("missing events: %+v", evs)
	}
}

func TestEngineMaxIters(t *testing.T) {
	// model always returns a tool call → never ends → hit MaxIters
	m := &fakeModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "echo", `{"msg":"x"}`))},
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t2", "echo", `{"msg":"x"}`))},
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t3", "echo", `{"msg":"x"}`))},
	}}
	eng := newEngineForTest(m, echoToolkit(), bypassEngine(), 2)
	evs := drain(eng.Run(context.Background(), "loop"))
	var re event.ReplyEnd
	for _, ev := range evs {
		if r, ok := ev.(event.ReplyEnd); ok {
			re = r
		}
	}
	if re.Reason != "max_iters" {
		t.Fatalf("reason: %s", re.Reason)
	}
}

func TestEngineCancel(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("x"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	evs := drain(eng.Run(ctx, "hi"))
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	last := evs[len(evs)-1]
	re, ok := last.(event.ReplyEnd)
	if !ok || (re.Reason != "cancelled" && re.Reason != "end_turn") {
		t.Fatalf("last: %+v", last)
	}
}

// recordingModel is a fake ChatModel that records the msgs passed to each
// ChatStream call (to assert multi-turn conversation persistence).
type recordingModel struct {
	turns [][]model.ChatResponse
	calls [][]*message.Msg
}

func (r *recordingModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	return nil, errRecordingChat
}

func (r *recordingModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	r.calls = append(r.calls, msgs)
	i := len(r.calls) - 1
	if i >= len(r.turns) {
		return nil, errRecordingNoTurns
	}
	chunks := r.turns[i]
	ch := make(chan model.ChatResponse, len(chunks))
	go func() {
		defer close(ch)
		for _, c := range chunks {
			ch <- c
		}
	}()
	return ch, nil
}

func (r *recordingModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int { return 0 }

var (
	errRecordingChat    = errors.New("recordingModel: Chat not used")
	errRecordingNoTurns = errors.New("recordingModel: no more scripted turns")
)

func TestEngineMultiTurnConversationPersists(t *testing.T) {
	m := &recordingModel{turns: [][]model.ChatResponse{
		{textChunk("hello"), finalChunk(&model.ChatUsage{})},
		{textChunk("ok"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	for range eng.Run(context.Background(), "first") {
	}
	for range eng.Run(context.Background(), "second") {
	}
	if len(m.calls) != 2 {
		t.Fatalf("ChatStream calls: %d", len(m.calls))
	}
	blob := ""
	for _, mm := range m.calls[1] {
		blob += string(mm.Role) + ":"
		if txt := mm.GetTextContent(" "); txt != nil {
			blob += *txt
		}
		blob += "\n"
	}
	for _, want := range []string{"first", "hello", "second"} {
		if !strings.Contains(blob, want) {
			t.Fatalf("turn-2 conv missing %q:\n%s", want, blob)
		}
	}
}

func TestEngineAutoCompactEmitsEvent(t *testing.T) {
	m := &compressFakeModel{tokenCount: 200000} // over threshold → auto-compress
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	eng.conv = append(eng.conv, message.UserMsg("u", "old1"), message.UserMsg("u", "old2"))
	evs := drain(eng.Run(context.Background(), "go"))
	var sawCompacted bool
	for _, ev := range evs {
		if _, ok := ev.(event.Compacted); ok {
			sawCompacted = true
		}
	}
	if !sawCompacted {
		t.Fatalf("expected Compacted event in: %+v", evs)
	}
}

func TestEnginePersistsNewSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("marker-response"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	sess, err := session.New("/p", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SaveMeta(); err != nil {
		t.Fatal(err)
	}
	eng.session = sess
	for range eng.Run(context.Background(), "marker-prompt") {
	}
	data, err := os.ReadFile(sess.Path)
	if err != nil {
		t.Fatal(err)
	}
	blob := string(data)
	if !strings.Contains(blob, "marker-prompt") || !strings.Contains(blob, "marker-response") {
		t.Fatalf("JSONL missing turn content:\n%s", blob)
	}
}

func TestEngineSkillToolReturnsBody(t *testing.T) {
	skills := []skill.Skill{{Name: "demo", Description: "d", Markdown: "DEMO-BODY-TEXT"}}
	tk := tool.NewToolkit(skill.NewSkillViewerTool(skills))
	m := &fakeModel{turns: [][]model.ChatResponse{
		// turn 1: model calls the Skill tool
		{finalChunk(&model.ChatUsage{}, toolCallBlock("s1", "Skill", `{"skill":"demo"}`))},
		// turn 2: final text
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tk, bypassEngine(), 10)
	evs := drain(eng.Run(context.Background(), "use the demo skill"))

	var sawResult bool
	for _, ev := range evs {
		if tr, ok := ev.(event.ToolResult); ok && tr.Name == "Skill" {
			sawResult = true
			if tr.State != "success" {
				t.Fatalf("Skill tool state: %s", tr.State)
			}
			if !strings.Contains(tr.Output, "DEMO-BODY-TEXT") {
				t.Fatalf("Skill tool output missing body: %q", tr.Output)
			}
		}
	}
	if !sawResult {
		t.Fatalf("no Skill ToolResult event in: %+v", evs)
	}
}

// mockMCPClient is a minimal mcp.Client for lifecycle tests.
type mockMCPClient struct {
	closed bool
}

func (m *mockMCPClient) ListTools(ctx context.Context) ([]model.ToolSchema, error) {
	return nil, nil
}
func (m *mockMCPClient) CallTool(ctx context.Context, name string, input map[string]any) (*tool.ToolResponse, error) {
	return nil, nil
}
func (m *mockMCPClient) Close() error {
	m.closed = true
	return nil
}

func TestEngineCloseNoClients(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10)
	if err := eng.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEngineCloseCallsClients(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10)
	c1, c2 := &mockMCPClient{}, &mockMCPClient{}
	eng.mcpClients = []mcp.Client{c1, c2}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	if !c1.closed || !c2.closed {
		t.Fatal("clients not closed")
	}
	if err := eng.Close(); err != nil { // idempotent
		t.Fatalf("second close: %v", err)
	}
}

func TestEngineUserPromptSubmitHookInjectsContext(t *testing.T) {
	m := &recordingModel{turns: [][]model.ChatResponse{
		{textChunk("ok"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	eng.hookRunner = hooks.NewRunner(map[string][]settings.Matcher{
		"UserPromptSubmit": {{Hooks: []settings.Command{{Type: "command", Command: `printf '{"additionalContext":"CTX"}'`}}}},
	}, "/tmp", "")
	for range eng.Run(context.Background(), "hello") {
	}
	if len(m.calls) != 1 {
		t.Fatalf("calls: %d", len(m.calls))
	}
	blob := ""
	for _, mm := range m.calls[0] {
		blob += string(mm.Role) + ":"
		if txt := mm.GetTextContent(" "); txt != nil {
			blob += *txt
		}
		blob += "\n"
	}
	if !strings.Contains(blob, "hello") || !strings.Contains(blob, "CTX") {
		t.Fatalf("user msg missing prompt/context: %s", blob)
	}
}

func TestEngineStopHookNoCrash(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	eng.hookRunner = hooks.NewRunner(map[string][]settings.Matcher{
		"Stop": {{Hooks: []settings.Command{{Type: "command", Command: "true"}}}},
	}, "/tmp", "")
	evs := drain(eng.Run(context.Background(), "hi"))
	last := evs[len(evs)-1]
	re, ok := last.(event.ReplyEnd)
	if !ok || re.Reason != "end_turn" {
		t.Fatalf("last event: %+v", last)
	}
}

// TestEngineToolResultAppendedAsUserRole guards the live-found bug where tool
// results were appended as assistant-role, making them invisible to Anthropic
// (which requires tool_result in a user message). The 2nd ChatStream call must
// see a user-role message carrying tool_result blocks.
func TestEngineToolResultAppendedAsUserRole(t *testing.T) {
	m := &recordingModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "echo", `{"msg":"hi"}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, echoToolkit(), bypassEngine(), 10)
	for range eng.Run(context.Background(), "call echo") {
	}
	if len(m.calls) != 2 {
		t.Fatalf("ChatStream calls: %d", len(m.calls))
	}
	var sawUserRoleToolResult bool
	var roles []string
	for _, mm := range m.calls[1] {
		roles = append(roles, string(mm.Role))
		if string(mm.Role) == "user" && mm.HasContentBlocks(message.ContentBlockToolResult) {
			sawUserRoleToolResult = true
		}
	}
	if !sawUserRoleToolResult {
		t.Fatalf("no user-role tool_result message in 2nd ChatStream call; roles=%v", roles)
	}
}
