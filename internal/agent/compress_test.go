package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
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
