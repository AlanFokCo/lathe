package agent

import (
	"context"
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
