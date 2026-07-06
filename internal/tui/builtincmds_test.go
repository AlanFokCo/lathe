package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/config"
)

func TestCostText(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.cumIn, m.cumOut, m.cumCacheR, m.cumCacheW = 10, 5, 3, 7
	got := m.costText()
	if !strings.Contains(got, "in=10 out=5") || !strings.Contains(got, "read=3 write=7") {
		t.Fatalf("costText = %q", got)
	}
}

func TestDoctorText(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, &config.Config{
		Provider: "openai", Model: "gpt-4o", APIKey: "sk-secret123", Permission: "accept_edits",
	})
	got := m.doctorText()
	for _, want := range []string{"provider:", "openai", "model:", "gpt-4o", "theme:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctorText missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret123") {
		t.Fatalf("doctorText leaked the api key:\n%s", got)
	}
}

func TestHandleInitWritesAndGuards(t *testing.T) {
	dir := t.TempDir()
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.cwd = dir
	m.handleInit()
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	// second call must not overwrite; reports "already exists"
	m.handleInit()
	if !strings.Contains(m.View(), "already exists") {
		t.Fatalf("second /init should report already-exists:\n%s", m.View())
	}
}
