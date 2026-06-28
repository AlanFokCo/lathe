package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/event"
	"github.com/alanfokco/lathe/internal/hooks"
	"github.com/alanfokco/lathe/internal/settings"
)

// echoTool returns its input text; carries a diff in metadata to test extraction.
func echoTool() tool.Tool {
	return tool.NewFunctionTool("echo", "echo back",
		json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			msg, _ := input["msg"].(string)
			return tool.NewTextResponse("echoed: " + msg), nil
		},
	)
}

func collect() (func(event.Event), *[]event.Event) {
	var evs []event.Event
	return func(e event.Event) { evs = append(evs, e) }, &evs
}

func TestDispatchBypassExecutes(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeBypass))
	emit, evs := collect()

	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, false, nil, emit)

	if len(results) != 1 || results[0].ID != "t1" || results[0].State != message.ToolResultSuccess {
		t.Fatalf("result: %+v", results)
	}
	out, _ := results[0].Output.(string)
	if !strings.HasPrefix(out, "echoed: hi") {
		t.Fatalf("output: %v", results[0].Output)
	}
	if len(*evs) != 2 {
		t.Fatalf("events: %+v", *evs)
	}
	if _, ok := (*evs)[0].(event.ToolCallStart); !ok {
		t.Fatalf("first event not ToolCallStart: %+v", *evs)
	}
}

func TestDispatchExploreDeniesNonReadOnly(t *testing.T) {
	tk := tool.NewToolkit(echoTool()) // echo is not IsReadOnly
	eng := permission.NewEngine(permission.NewContext(permission.ModeExplore))
	emit, _ := collect()

	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, false, nil, emit)

	if results[0].State != message.ToolResultDenied {
		t.Fatalf("expected denied, got %s", results[0].State)
	}
}

func TestDispatchUnknownTool(t *testing.T) {
	tk := tool.NewToolkit()
	eng := permission.NewEngine(permission.NewContext(permission.ModeBypass))
	emit, _ := collect()
	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "nope", `{}`)},
		tk, eng, false, nil, emit)
	if results[0].State != message.ToolResultError {
		t.Fatalf("expected error, got %s", results[0].State)
	}
}

func TestDispatchInteractiveAllow(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	approvalCh := make(chan string, 1)
	var mu sync.Mutex
	var evs []event.Event
	emit := func(e event.Event) {
		mu.Lock()
		evs = append(evs, e)
		mu.Unlock()
		if _, ok := e.(event.RequireApproval); ok {
			approvalCh <- "allow"
		}
	}
	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, true, approvalCh, emit)
	if results[0].State != message.ToolResultSuccess {
		t.Fatalf("expected success after allow: %s", results[0].State)
	}
}

func TestDispatchInteractiveDeny(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	approvalCh := make(chan string, 1)
	emit := func(e event.Event) {
		if _, ok := e.(event.RequireApproval); ok {
			approvalCh <- "deny"
		}
	}
	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, true, approvalCh, emit)
	if results[0].State != message.ToolResultDenied {
		t.Fatalf("expected denied: %s", results[0].State)
	}
}

func TestDispatchInteractiveAlwaysAddsRule(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	approvalCh := make(chan string, 1)
	emit := func(e event.Event) {
		if _, ok := e.(event.RequireApproval); ok {
			approvalCh <- "always"
		}
	}
	r1 := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, true, approvalCh, emit)
	if r1[0].State != message.ToolResultSuccess {
		t.Fatalf("first call state: %s", r1[0].State)
	}
	var sawRequire bool
	emit2 := func(e event.Event) {
		if _, ok := e.(event.RequireApproval); ok {
			sawRequire = true
		}
	}
	r2 := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t2", "echo", `{"msg":"yo"}`)},
		tk, eng, true, make(chan string, 1), emit2)
	if r2[0].State != message.ToolResultSuccess {
		t.Fatalf("second call state: %s", r2[0].State)
	}
	if sawRequire {
		t.Fatal("second call should not ask (always rule added)")
	}
}

func TestDispatchInteractiveCtxCancel(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	approvalCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := dispatch(ctx,
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, true, approvalCh, func(event.Event) {})
	if results[0].State != message.ToolResultDenied {
		t.Fatalf("expected denied on ctx cancel: %s", results[0].State)
	}
}

func TestDispatchPreToolUseBlock(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeBypass))
	runner := hooks.NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "echo", Hooks: []settings.Command{{Type: "command", Command: `printf '{"decision":"block","reason":"no"}'`}}}},
	}, "/tmp", "")
	emit, _ := collect()
	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, false, nil, emit, runner)
	if results[0].State != message.ToolResultDenied {
		t.Fatalf("PreToolUse block should deny: %s", results[0].State)
	}
	out, _ := results[0].Output.(string)
	if !strings.Contains(out, "blocked by hook") {
		t.Fatalf("output: %q", out)
	}
}

func TestDispatchPostToolUseContext(t *testing.T) {
	tk := tool.NewToolkit(echoTool())
	eng := permission.NewEngine(permission.NewContext(permission.ModeBypass))
	runner := hooks.NewRunner(map[string][]settings.Matcher{
		"PostToolUse": {{Matcher: "echo", Hooks: []settings.Command{{Type: "command", Command: `printf '{"additionalContext":"extra"}'`}}}},
	}, "/tmp", "")
	emit, _ := collect()
	results := dispatch(context.Background(),
		[]message.ToolCallBlock{toolCallBlock("t1", "echo", `{"msg":"hi"}`)},
		tk, eng, false, nil, emit, runner)
	out, _ := results[0].Output.(string)
	if !strings.Contains(out, "echoed: hi") || !strings.Contains(out, "extra") {
		t.Fatalf("PostToolUse context not appended: %q", out)
	}
}
