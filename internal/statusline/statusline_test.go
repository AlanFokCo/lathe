package statusline

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestBuildJSON(t *testing.T) {
	in := Input{
		SessionID: "abc", TranscriptPath: "/p/abc.jsonl", Cwd: "/work",
		Model: "gpt-4o", Version: "0.1.0-dev",
		ContextSize: 128000, InputTokens: 1280, OutputTokens: 50,
	}
	var m map[string]any
	if err := json.Unmarshal(BuildJSON(in), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["session_id"] != "abc" {
		t.Errorf("session_id: %v", m["session_id"])
	}
	if m["transcript_path"] != "/p/abc.jsonl" {
		t.Errorf("transcript_path: %v", m["transcript_path"])
	}
	if m["cwd"] != "/work" {
		t.Errorf("cwd: %v", m["cwd"])
	}
	if m["version"] != "0.1.0-dev" {
		t.Errorf("version: %v", m["version"])
	}
	mdl, _ := m["model"].(map[string]any)
	if mdl["id"] != "gpt-4o" || mdl["display_name"] != "gpt-4o" {
		t.Errorf("model: %v", mdl)
	}
	ws, _ := m["workspace"].(map[string]any)
	if ws["current_dir"] != "/work" || ws["project_dir"] != "/work" {
		t.Errorf("workspace: %v", ws)
	}
	cw, _ := m["context_window"].(map[string]any)
	if cw == nil {
		t.Fatal("context_window missing")
	}
	if cw["total_input_tokens"] != float64(1280) {
		t.Errorf("total_input_tokens: %v", cw["total_input_tokens"])
	}
	if cw["total_output_tokens"] != float64(50) {
		t.Errorf("total_output_tokens: %v", cw["total_output_tokens"])
	}
	if cw["context_window_size"] != float64(128000) {
		t.Errorf("context_window_size: %v", cw["context_window_size"])
	}
	if cw["used_percentage"] == nil {
		t.Error("used_percentage missing")
	}
	if m["exceeds_200k_tokens"] != false {
		t.Errorf("exceeds_200k: %v", m["exceeds_200k_tokens"])
	}
}

func TestBuildJSON_Exceeds200k(t *testing.T) {
	in := Input{Cwd: "/w", Model: "m", Version: "v", ContextSize: 300000, InputTokens: 200001}
	var m map[string]any
	json.Unmarshal(BuildJSON(in), &m)
	if m["exceeds_200k_tokens"] != true {
		t.Errorf("want exceeds_200k=true, got %v", m["exceeds_200k_tokens"])
	}
}

func TestBuildJSON_OmitsContextWindowWhenZero(t *testing.T) {
	in := Input{SessionID: "s", Cwd: "/w", Model: "m", Version: "v", ContextSize: 0, InputTokens: 5}
	var m map[string]any
	json.Unmarshal(BuildJSON(in), &m)
	if _, ok := m["context_window"]; ok {
		t.Error("context_window should be omitted when ContextSize<=0")
	}
	if m["exceeds_200k_tokens"] != false {
		t.Errorf("exceeds_200k still emitted: %v", m["exceeds_200k_tokens"])
	}
}

func TestRun_Success(t *testing.T) {
	cfg := Config{Type: "command", Command: "echo hello-status"}
	got, err := Run(context.Background(), cfg, Input{})
	if err != nil || got != "hello-status" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestRun_MultiLineTrimsDropsEmpty(t *testing.T) {
	cfg := Config{Type: "command", Command: "printf ' a\\n\\nb \\n'"}
	got, err := Run(context.Background(), cfg, Input{})
	if err != nil || got != "a\nb" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	cfg := Config{Type: "command", Command: "exit 1"}
	got, err := Run(context.Background(), cfg, Input{})
	if err == nil || got != "" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestRun_Timeout(t *testing.T) {
	cfg := Config{Type: "command", Command: "sleep 10"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := Run(ctx, cfg, Input{})
	if err == nil || got != "" {
		t.Fatalf("got %q err %v (want timeout)", got, err)
	}
}

func TestRun_NoCommand(t *testing.T) {
	got, err := Run(context.Background(), Config{Type: "", Command: ""}, Input{})
	if err != nil || got != "" {
		t.Fatalf("got %q err %v", got, err)
	}
}
