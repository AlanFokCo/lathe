package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/session"
)

func TestEngineSetModelAndModelName(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10)
	if eng.ModelName() != "test-model" {
		t.Fatalf("default model: %s", eng.ModelName())
	}
	if err := eng.SetModel("gpt-4o"); err != nil {
		t.Fatalf("setmodel: %v", err)
	}
	if eng.ModelName() != "gpt-4o" {
		t.Fatalf("after set: %s", eng.ModelName())
	}
}

func TestEngineListModelsForProvider(t *testing.T) {
	eng := newEngineForTest(&fakeModel{}, tool.NewToolkit(), bypassEngine(), 10) // provider=openai
	models := eng.ListModels()
	if len(models) == 0 {
		t.Fatal("no models listed")
	}
	for _, m := range models {
		if strings.Contains(strings.ToLower(m), "claude") {
			t.Fatalf("openai list has claude model %q", m)
		}
	}
}

func TestNewEngineResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// pre-create a session with one user msg
	sess, err := session.New("/p", "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SaveMeta(); err != nil {
		t.Fatal(err)
	}
	if err := sess.Save(message.UserMsg("u", "previous turn")); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "accept_edits", MaxIters: 10, Resume: sess.ID}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newengine: %v", err)
	}
	blob := ""
	for _, m := range eng.conv {
		if txt := m.GetTextContent(" "); txt != nil {
			blob += *txt
		}
	}
	if !strings.Contains(blob, "previous turn") {
		t.Fatalf("resumed conv missing history:\n%s", blob)
	}
}

func TestNewEngineRegistersSkillToolWhenSkillsExist(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj") // under home → project walk stops at home
	mustMkdir(t, work)
	skillDir := filepath.Join(work, ".lathe", "skills", "demo")
	mustMkdir(t, skillDir)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: demo\ndescription: a demo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "accept_edits", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if eng.toolkit.Get("Skill") == nil {
		t.Fatal("Skill tool not registered when a skill exists")
	}
}

func TestNewEngineNoSkillToolWhenAbsent(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj2")
	mustMkdir(t, work)
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "accept_edits", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if eng.toolkit.Get("Skill") != nil {
		t.Fatal("Skill tool should not be registered when no skills exist")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func startMockMCPServer(t *testing.T, toolName string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "mock", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": toolName, "description": "mock", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + l.Addr().String()
}

func writeMCPJSONFile(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	b, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewEngineRegistersMCPTool(t *testing.T) {
	url := startMockMCPServer(t, "mcpdemo")
	home := t.TempDir()
	work := filepath.Join(home, "proj") // under home → project walk / user-level resolve cleanly
	mustMkdir(t, work)
	writeMCPJSONFile(t, filepath.Join(work, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"m": map[string]any{"type": "http", "url": url}}})
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "bypass", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.toolkit.Get("mcpdemo") == nil {
		t.Fatal("MCP tool not registered")
	}
}

func TestNewEngineNoMCPWhenAbsent(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj2")
	mustMkdir(t, work)
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "bypass", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.toolkit.Get("mcpdemo") != nil {
		t.Fatal("no MCP tool expected when .mcp.json absent")
	}
}

func writeSettingsFile(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	b, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewEngineWiresHookSettings(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj")
	mustMkdir(t, work)
	writeSettingsFile(t, filepath.Join(work, ".lathe", "settings.json"), map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": `printf '{"decision":"block","reason":"x"}'`}}}},
	}})
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "bypass", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.hookRunner == nil {
		t.Fatal("hookRunner not set")
	}
	r, _ := eng.hookRunner.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "Bash"})
	if !r.Block || r.Reason != "x" {
		t.Fatalf("runner not wired from settings: %+v", r)
	}
}

func TestNewEngineRegistersTaskTool(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj")
	mustMkdir(t, work)
	t.Setenv("HOME", home)
	t.Chdir(work)

	cfg := &config.Config{Provider: "openai", Model: "gpt-4o", APIKey: "k", Permission: "bypass", MaxIters: 10}
	eng, err := NewEngine(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if eng.toolkit.Get("Task") == nil {
		t.Fatal("Task tool not registered")
	}
}
