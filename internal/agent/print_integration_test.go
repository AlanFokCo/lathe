package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/render"
)

// TestPrintIntegration wires a fake-model engine through the text renderer,
// validating the full engine→event→render pipeline without a real API.
func TestPrintIntegration(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("Hello "), textChunk("world"), finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 2})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(),
		permission.NewEngine(permission.NewContext(permission.ModeBypass)), 10)

	ch := eng.Run(context.Background(), "say hi")
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	render.RenderText(context.Background(), ch, out, errOut)

	if !strings.HasPrefix(out.String(), "Hello world") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestPrintIntegrationStreamJSON(t *testing.T) {
	m := &fakeModel{turns: [][]model.ChatResponse{
		{textChunk("hi"), finalChunk(&model.ChatUsage{InputTokens: 1, OutputTokens: 1})},
	}}
	eng := newEngineForTest(m, tool.NewToolkit(),
		permission.NewEngine(permission.NewContext(permission.ModeBypass)), 10)

	ch := eng.Run(context.Background(), "hi")
	out := &bytes.Buffer{}
	render.RenderStreamJSON(context.Background(), ch, out)
	if !strings.Contains(out.String(), `"type":"text_delta"`) {
		t.Fatalf("output: %s", out.String())
	}
	// sanity: ensure the first line decodes as JSON
	var obj map[string]any
	if err := json.NewDecoder(bytes.NewReader(out.Bytes())).Decode(&obj); err != nil {
		t.Fatalf("first line not JSON: %v", err)
	}
}
