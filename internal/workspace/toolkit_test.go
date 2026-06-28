package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/workspace"
)

// mockWorkspace implements workspace.Workspace for tests.
type mockWorkspace struct {
	basePath   string
	files      map[string][]byte
	execCmds   []string
	execResult workspace.ExecResult
	execErr    error
}

func newMockWorkspace() *mockWorkspace {
	return &mockWorkspace{basePath: "/ws", files: map[string][]byte{}}
}

func (m *mockWorkspace) WriteFile(_ context.Context, path string, data []byte) error {
	m.files[path] = data
	return nil
}
func (m *mockWorkspace) ReadFile(_ context.Context, path string) ([]byte, error) {
	if d, ok := m.files[path]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", path)
}
func (m *mockWorkspace) ListFiles(_ context.Context, dir string) ([]workspace.FileInfo, error) {
	return nil, nil
}
func (m *mockWorkspace) RemoveFile(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}
func (m *mockWorkspace) Execute(_ context.Context, command string) (*workspace.ExecResult, error) {
	m.execCmds = append(m.execCmds, command)
	if m.execErr != nil {
		return nil, m.execErr
	}
	return &m.execResult, nil
}
func (m *mockWorkspace) BasePath() string { return m.basePath }

func extractText(resp *tool.ToolResponse) string {
	got := ""
	for _, b := range resp.Content {
		if tb, ok := b.(message.TextBlock); ok {
			got += tb.Text
		}
	}
	return got
}

func TestBashTool(t *testing.T) {
	ws := newMockWorkspace()
	ws.execResult = workspace.ExecResult{Stdout: "hello\n", ExitCode: 0}
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Bash").Execute(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	got := extractText(resp)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "exit_code") {
		t.Fatalf("bash output: %q", got)
	}
	if len(ws.execCmds) != 1 || ws.execCmds[0] != "echo hello" {
		t.Fatalf("exec commands: %v", ws.execCmds)
	}
}

func TestReadTool(t *testing.T) {
	ws := newMockWorkspace()
	ws.files["foo.txt"] = []byte("file content")
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Read").Execute(context.Background(), map[string]any{"path": "foo.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); got != "file content" {
		t.Fatalf("read output: %q", got)
	}
}

func TestWriteTool(t *testing.T) {
	ws := newMockWorkspace()
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Write").Execute(context.Background(), map[string]any{"path": "out.txt", "content": "data"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); got != "ok" {
		t.Fatalf("write output: %q", got)
	}
	if string(ws.files["out.txt"]) != "data" {
		t.Fatalf("file not written: %q", ws.files["out.txt"])
	}
}

func TestEditTool(t *testing.T) {
	ws := newMockWorkspace()
	ws.files["f.txt"] = []byte("hello world")
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Edit").Execute(context.Background(), map[string]any{"path": "f.txt", "old_string": "hello", "new_string": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); got != "ok" {
		t.Fatalf("edit output: %q", got)
	}
	if string(ws.files["f.txt"]) != "hi world" {
		t.Fatalf("file not edited: %q", ws.files["f.txt"])
	}
	// old_string not found → error in ToolResponse
	resp, _ = tk.Get("Edit").Execute(context.Background(), map[string]any{"path": "f.txt", "old_string": "nope", "new_string": "x"})
	if got := extractText(resp); !strings.Contains(got, "not found") {
		t.Fatalf("want not-found error, got %q", got)
	}
	// ambiguous → error
	ws.files["a.txt"] = []byte("x x")
	resp, _ = tk.Get("Edit").Execute(context.Background(), map[string]any{"path": "a.txt", "old_string": "x", "new_string": "y"})
	if got := extractText(resp); !strings.Contains(got, "times") {
		t.Fatalf("want ambiguous error, got %q", got)
	}
}

func TestGlobTool(t *testing.T) {
	ws := newMockWorkspace()
	ws.execResult = workspace.ExecResult{Stdout: "/ws/a.go\n/ws/b.go\n"}
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Glob").Execute(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); !strings.Contains(got, "a.go") {
		t.Fatalf("glob output: %q", got)
	}
	if len(ws.execCmds) != 1 || !strings.Contains(ws.execCmds[0], "find") || !strings.Contains(ws.execCmds[0], "*.go") {
		t.Fatalf("exec command: %v", ws.execCmds)
	}
}

func TestGrepTool(t *testing.T) {
	ws := newMockWorkspace()
	ws.execResult = workspace.ExecResult{Stdout: "/ws/a.go:1:foo\n"}
	tk := WorkspaceToolkit(ws)
	resp, err := tk.Get("Grep").Execute(context.Background(), map[string]any{"pattern": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractText(resp); !strings.Contains(got, "foo") {
		t.Fatalf("grep output: %q", got)
	}
	if len(ws.execCmds) != 1 || !strings.Contains(ws.execCmds[0], "grep") {
		t.Fatalf("exec command: %v", ws.execCmds)
	}
}
