package mcpconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeMCPJSON(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	b, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigStdio(t *testing.T) {
	cwd := t.TempDir()
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"fs": map[string]any{"command": "npx", "args": []any{"-y", "srv"}, "env": map[string]any{"K": "V"}, "cwd": "/opt"},
		},
	})
	cfgs, w := LoadConfig(cwd)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(cfgs) != 1 || cfgs[0].Name != "fs" || cfgs[0].Command != "npx" || cfgs[0].Type != "stdio" {
		t.Fatalf("cfg: %+v", cfgs)
	}
	if len(cfgs[0].Args) != 2 || cfgs[0].Args[0] != "-y" {
		t.Fatalf("args: %+v", cfgs[0].Args)
	}
	found := false
	for _, e := range cfgs[0].Env {
		if e == "K=V" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env: %+v", cfgs[0].Env)
	}
	if cfgs[0].WorkDir != "/opt" {
		t.Fatalf("cwd: %s", cfgs[0].WorkDir)
	}
}

func TestLoadConfigHTTP(t *testing.T) {
	cwd := t.TempDir()
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"remote": map[string]any{"type": "http", "url": "http://x/mcp", "headers": map[string]any{"Authorization": "Bearer y"}},
		},
	})
	cfgs, w := LoadConfig(cwd)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(cfgs) != 1 || cfgs[0].Type != "http" || cfgs[0].URL != "http://x/mcp" {
		t.Fatalf("cfg: %+v", cfgs)
	}
	if cfgs[0].Headers["Authorization"] != "Bearer y" {
		t.Fatalf("headers: %+v", cfgs[0].Headers)
	}
}

func TestLoadConfigProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	writeMCPJSON(t, filepath.Join(home, ".lathe", "mcp.json"), map[string]any{"mcpServers": map[string]any{"s": map[string]any{"command": "user-cmd"}}})
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"s": map[string]any{"command": "proj-cmd"}}})
	cfgs, w := LoadConfig(cwd)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(cfgs) != 1 || cfgs[0].Command != "proj-cmd" {
		t.Fatalf("project should override user: %+v", cfgs)
	}
}

func TestLoadConfigSSESkipped(t *testing.T) {
	cwd := t.TempDir()
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"s": map[string]any{"type": "sse", "url": "http://x"}}})
	cfgs, w := LoadConfig(cwd)
	if len(cfgs) != 0 {
		t.Fatalf("sse should be skipped: %+v", cfgs)
	}
	if len(w) != 1 {
		t.Fatalf("want 1 warning, got %v", w)
	}
}

func TestLoadConfigInvalidName(t *testing.T) {
	cwd := t.TempDir()
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"bad name": map[string]any{"command": "x"}}})
	cfgs, w := LoadConfig(cwd)
	if len(cfgs) != 0 {
		t.Fatalf("invalid name should be skipped: %+v", cfgs)
	}
	if len(w) != 1 {
		t.Fatalf("want 1 warning, got %v", w)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgs, w := LoadConfig(t.TempDir())
	if len(cfgs) != 0 || len(w) != 0 {
		t.Fatalf("want empty, got cfgs=%+v w=%v", cfgs, w)
	}
}

func TestLoadConfigBadJSON(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".mcp.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgs, w := LoadConfig(cwd)
	if len(cfgs) != 0 {
		t.Fatalf("want no cfgs on bad json: %+v", cfgs)
	}
	if len(w) != 1 {
		t.Fatalf("want 1 warning, got %v", w)
	}
}

func TestLoadConfigStdioMissingCommand(t *testing.T) {
	cwd := t.TempDir()
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"s": map[string]any{"args": []any{"x"}}}})
	cfgs, w := LoadConfig(cwd)
	if len(cfgs) != 0 {
		t.Fatalf("want skip: %+v", cfgs)
	}
	if len(w) != 1 {
		t.Fatalf("want 1 warning, got %v", w)
	}
}
