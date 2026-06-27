package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/event"
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
		tk, eng, emit)

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
		tk, eng, emit)

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
		tk, eng, emit)
	if results[0].State != message.ToolResultError {
		t.Fatalf("expected error, got %s", results[0].State)
	}
}
