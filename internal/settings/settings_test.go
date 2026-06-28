package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	b, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadHooksShape(t *testing.T) {
	cwd := t.TempDir()
	writeSettings(t, filepath.Join(cwd, ".lathe", "settings.json"), map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "echo hi", "timeout": 10}}},
			},
		},
	})
	s, err := Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	m := s.Hooks["PreToolUse"]
	if len(m) != 1 || m[0].Matcher != "Bash" || len(m[0].Hooks) != 1 {
		t.Fatalf("matchers: %+v", m)
	}
	if m[0].Hooks[0].Type != "command" || m[0].Hooks[0].Command != "echo hi" || m[0].Hooks[0].Timeout != 10 {
		t.Fatalf("command: %+v", m[0].Hooks[0])
	}
}

func TestLoadProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	writeSettings(t, filepath.Join(home, ".lathe", "settings.json"), map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"command": "user"}}}},
	}})
	writeSettings(t, filepath.Join(cwd, ".lathe", "settings.json"), map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"command": "proj"}}}},
	}})
	s, err := Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hooks["PreToolUse"][0].Hooks[0].Command != "proj" {
		t.Fatalf("project should override user: %+v", s.Hooks["PreToolUse"])
	}
}

func TestLoadMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := Load(t.TempDir())
	if err != nil || s == nil || len(s.Hooks) != 0 {
		t.Fatalf("want empty settings, got %+v %v", s, err)
	}
}

func TestLoadBadJSON(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".lathe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".lathe", "settings.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cwd); err == nil {
		t.Fatal("want error on bad json")
	}
}

func TestLoadStatusLine(t *testing.T) {
	cwd := t.TempDir()
	writeSettings(t, filepath.Join(cwd, ".lathe", "settings.json"), map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "echo hi", "padding": 2},
	})
	s, err := Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if s.StatusLine == nil || s.StatusLine.Type != "command" ||
		s.StatusLine.Command != "echo hi" || s.StatusLine.Padding != 2 {
		t.Fatalf("StatusLine: %+v", s.StatusLine)
	}
}

func TestLoadStatusLine_ProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	writeSettings(t, filepath.Join(home, ".lathe", "settings.json"), map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "user-cmd"},
	})
	writeSettings(t, filepath.Join(cwd, ".lathe", "settings.json"), map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "proj-cmd", "padding": 1},
	})
	s, err := Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if s.StatusLine == nil || s.StatusLine.Command != "proj-cmd" || s.StatusLine.Padding != 1 {
		t.Fatalf("project should override user: %+v", s.StatusLine)
	}
}

func TestLoadStatusLine_Absent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := Load(t.TempDir())
	if err != nil || s.StatusLine != nil {
		t.Fatalf("want nil StatusLine, got %+v %v", s, err)
	}
}
