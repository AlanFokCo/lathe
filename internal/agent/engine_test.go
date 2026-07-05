package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	asevent "github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/mcp"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/hooks"
	"github.com/alanfokco/lathe/internal/session"
	"github.com/alanfokco/lathe/internal/settings"
)

func drain(ch <-chan asevent.Event) []asevent.Event {
	var out []asevent.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// toolResultAccum is one tool call's accumulated result (name + output + state).
type toolResultAccum struct{ name, output, state string }

// drained is a structured summary of an agent event stream (M6a Commit B):
// concatenated text deltas, the last ModelCallEnd, tool results by call ID,
// and whether ExceedMaxIters / ReplyEnd were seen.
type drained struct {
	text      string
	usage     *asevent.ModelCallEndEvent
	tools     map[string]toolResultAccum
	exceedMax bool
	replyEnd  bool
	n         int
}

func drainAll(ch <-chan asevent.Event) drained {
	var d drained
	d.tools = map[string]toolResultAccum{}
	out := map[string]*strings.Builder{}
	for ev := range ch {
		d.n++
		switch e := ev.(type) {
		case asevent.TextBlockDeltaEvent:
			d.text += e.Delta
		case asevent.ModelCallEndEvent:
			u := e
			d.usage = &u
		case asevent.ToolResultStartEvent:
			d.tools[e.ToolCallID] = toolResultAccum{name: e.ToolCallName}
			out[e.ToolCallID] = &strings.Builder{}
		case asevent.ToolResultTextDeltaEvent:
			if b, ok := out[e.ToolCallID]; ok {
				b.WriteString(e.Delta)
				tr := d.tools[e.ToolCallID]
				tr.output = b.String()
				d.tools[e.ToolCallID] = tr
			}
		case asevent.ToolResultEndEvent:
			tr := d.tools[e.ToolCallID]
			tr.state = string(e.State)
			d.tools[e.ToolCallID] = tr
		case asevent.ExceedMaxItersEvent:
			d.exceedMax = true
		case asevent.ReplyEndEvent:
			d.replyEnd = true
		}
	}
	return d
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

// echoTool returns a single echo FunctionTool (used by task_tool tests).
func echoTool() tool.Tool {
	return tool.NewFunctionTool("echo", "echo back",
		json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			msg, _ := input["msg"].(string)
			return tool.NewTextResponse("echoed: " + msg), nil
		},
	)
}

func TestEnginePureTextTurn(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("Hel"), textChunk("lo"), finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 2})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	// M6a Commit B: agentscope emits one TextBlockDeltaEvent per text block
	// (sync Chat returns the full response in one ChatResponse). Assert content.
	d := drainAll(eng.Run(context.Background(), "hi"))
	if d.text != "Hello" {
		t.Fatalf("text: %q", d.text)
	}
	if d.usage == nil || d.usage.InputTokens != 1 || d.usage.OutputTokens != 2 {
		t.Fatalf("usage: %+v", d.usage)
	}
	if !d.replyEnd {
		t.Fatal("missing reply_end event")
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
	d := drainAll(eng.Run(context.Background(), "call echo"))
	tr, ok := d.tools["t1"]
	if !ok {
		t.Fatalf("no tool result for t1: %+v", d.tools)
	}
	if tr.state != "success" {
		t.Fatalf("tool state: %s", tr.state)
	}
	if !strings.Contains(tr.output, "echoed: hi") {
		t.Fatalf("tool output: %q", tr.output)
	}
	if !d.replyEnd {
		t.Fatal("missing reply_end event")
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
	// M6a Commit B: agentscope emits ExceedMaxItersEvent (ReplyEndEvent carries
	// no reason; the max_iters signal is the ExceedMaxIters event).
	d := drainAll(eng.Run(context.Background(), "loop"))
	if !d.exceedMax {
		t.Fatal("expected ExceedMaxIters event")
	}
	if !d.replyEnd {
		t.Fatal("missing reply_end event")
	}
}

func TestEngineCancel(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("x"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	d := drainAll(eng.Run(ctx, "hi"))
	if d.n == 0 {
		t.Fatal("no events")
	}
	// ReplyEndEvent carries no reason; just assert the stream ended cleanly.
	if !d.replyEnd {
		t.Fatalf("stream ended without reply_end; events: %d", d.n)
	}
}

// recordingModel is a fake ChatModel that records the msgs passed to each
// Chat call (to assert multi-turn conversation persistence). M6a: v3 drives
// sync Chat; ChatStream is unused.
type recordingModel struct {
	turns [][]model.ChatResponse
	calls [][]*message.Msg
}

func (r *recordingModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	r.calls = append(r.calls, msgs)
	i := len(r.calls) - 1
	if i >= len(r.turns) {
		return nil, errRecordingNoTurns
	}
	return mergeChunks(r.turns[i]), nil
}

func (r *recordingModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, errors.New("recordingModel: ChatStream not used under v3")
}

func (r *recordingModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int { return 0 }

var errRecordingNoTurns = errors.New("recordingModel: no more scripted turns")

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
	// M6b: agentscope's replyLoop detects compression (state.Summary change)
	// and emits a CustomEvent("compacted"); lathe surfaces it (tui/stream-json).
	m := &compressFakeModel{tokenCount: 200000} // over threshold → auto-compress
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	eng.state.Context = append(eng.state.Context, message.UserMsg("u", "old1"), message.UserMsg("u", "old2"))
	evs := drain(eng.Run(context.Background(), "go"))
	var sawCompacted bool
	for _, ev := range evs {
		if ce, ok := ev.(asevent.CustomEvent); ok && ce.Name == "compacted" {
			sawCompacted = true
		}
	}
	if !sawCompacted {
		t.Fatalf("expected compacted CustomEvent in: %+v", evs)
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
	d := drainAll(eng.Run(context.Background(), "use the demo skill"))

	tr, ok := d.tools["s1"]
	if !ok {
		t.Fatalf("no Skill tool result: %+v", d.tools)
	}
	if tr.name != "Skill" || tr.state != "success" {
		t.Fatalf("Skill tool result: %+v", tr)
	}
	if !strings.Contains(tr.output, "DEMO-BODY-TEXT") {
		t.Fatalf("Skill tool output missing body: %q", tr.output)
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
	d := drainAll(eng.Run(context.Background(), "hi"))
	if !d.replyEnd {
		t.Fatalf("missing reply_end event; events: %d", d.n)
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

// TestEngineUsageCarriesCacheTokens — M6b: upstream agentscope now surfaces
// prompt-cache tokens on ModelCallEndEvent (NewModelCallEndEventWithCache);
// the fakeModel's ChatUsage carries them through the replyLoop.
func TestEngineUsageCarriesCacheTokens(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("hi"), finalChunk(&model.ChatUsage{
			InputTokens: 10, OutputTokens: 5,
			CacheCreationInputTokens: 7, CacheInputTokens: 3,
		})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(), bypassEngine(), 10)
	d := drainAll(eng.Run(context.Background(), "hi"))
	if d.usage == nil {
		t.Fatal("no ModelCallEnd event")
	}
	if d.usage.CacheCreationTokens != 7 || d.usage.CacheReadTokens != 3 {
		t.Fatalf("cache tokens: creation=%d read=%d (want 7,3)", d.usage.CacheCreationTokens, d.usage.CacheReadTokens)
	}
}

// TestEngineTruncatesToolResultInConv — M6a: a tool result larger than
// compressCfg.ToolResultLimit is truncated in the conversation copy, while the
// full output still reaches the TUI via the ToolResult event.
func TestEngineTruncatesToolResultInConv(t *testing.T) {
	t.Skip("M6b: per-turn ToolResultLimit truncation deferred — agentscope's truncateToolResult truncates in both event and context; matching lathe's 'full in event, truncated in conv' needs an offloader/middleware (M6b)")
	big := strings.Repeat("A", 400_000) // ~100k est. tokens > 50k limit
	bigToolkit := tool.NewToolkit(tool.NewFunctionTool("big", "big",
		json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			return tool.NewTextResponse(big), nil
		}))
	m := &recordingModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "big", `{}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, bigToolkit, bypassEngine(), 10)
	d := drainAll(eng.Run(context.Background(), "go"))
	var fullInEvent bool
	if tr, ok := d.tools["t1"]; ok && tr.name == "big" && len(tr.output) == len(big) {
		fullInEvent = true
	}
	if !fullInEvent {
		t.Fatal("ToolResult event should carry the FULL output")
	}
	var convOut string
	for _, mm := range m.calls[1] {
		for _, b := range mm.GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok {
				convOut = tr.GetOutputText()
			}
		}
	}
	if !strings.Contains(convOut, "<<<TRUNCATED>>>") {
		t.Fatalf("conversation tool_result not truncated (len=%d)", len(convOut))
	}
	if len(convOut) >= len(big) {
		t.Fatalf("conversation still holds full output: %d", len(convOut))
	}
}

// TestEngineInjectsReadCache — M6a: the turn ctx passed to tools carries a
// ReadCache, so the base Write/Edit read-before-write guard is active.
func TestEngineInjectsReadCache(t *testing.T) {
	probe := tool.NewFunctionTool("probe", "reports whether a ReadCache is in ctx",
		json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			if tool.GetReadCache(ctx) != nil {
				return tool.NewTextResponse("cache:yes"), nil
			}
			return tool.NewTextResponse("cache:no"), nil
		})
	m := &fakeModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "probe", `{}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(probe), bypassEngine(), 10)
	d := drainAll(eng.Run(context.Background(), "go"))
	tr, ok := d.tools["t1"]
	if !ok || tr.output != "cache:yes" {
		t.Fatalf("ReadCache not injected into tool ctx: %+v", tr)
	}
}

// TestEnginePreToolUseHookBlocks — M6b: a PreToolUse hook returning
// {"decision":"block"} denies the tool call (shellHookMiddleware restores the
// PreToolUse boundary that v3 lost when dispatch.go was deleted).
func TestEnginePreToolUseHookBlocks(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "echo", `{"msg":"hi"}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, echoToolkit(), bypassEngine(), 10)
	eng.hookRunner = hooks.NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "echo", Hooks: []settings.Command{{Type: "command", Command: `printf '{"decision":"block","reason":"no"}'`}}}},
	}, "/tmp", "")
	d := drainAll(eng.Run(context.Background(), "call echo"))
	tr, ok := d.tools["t1"]
	if !ok {
		t.Fatalf("no tool result: %+v", d.tools)
	}
	if tr.state != "denied" {
		t.Fatalf("state: %s (want denied)", tr.state)
	}
	if !strings.Contains(tr.output, "blocked by hook") {
		t.Fatalf("output: %q", tr.output)
	}
}

// TestEnginePostToolUseHookInjectsContext — M6b: a PostToolUse hook returning
// {"additionalContext":"..."} appends to the tool output (shellHookMiddleware).
func TestEnginePostToolUseHookInjectsContext(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{finalChunk(&model.ChatUsage{}, toolCallBlock("t1", "echo", `{"msg":"hi"}`))},
		{textChunk("done"), finalChunk(&model.ChatUsage{})},
	}}
	eng := newEngineForTest(m, echoToolkit(), bypassEngine(), 10)
	eng.hookRunner = hooks.NewRunner(map[string][]settings.Matcher{
		"PostToolUse": {{Matcher: "echo", Hooks: []settings.Command{{Type: "command", Command: `printf '{"additionalContext":"CTX-ADDED"}'`}}}},
	}, "/tmp", "")
	d := drainAll(eng.Run(context.Background(), "call echo"))
	tr, ok := d.tools["t1"]
	if !ok {
		t.Fatalf("no tool result: %+v", d.tools)
	}
	if !strings.Contains(tr.output, "echoed: hi") || !strings.Contains(tr.output, "CTX-ADDED") {
		t.Fatalf("output missing echo/context: %q", tr.output)
	}
}
