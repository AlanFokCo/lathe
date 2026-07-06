package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanfokco/lathe/internal/config"
	"github.com/alanfokco/lathe/internal/mcpconfig"
)

func TestCostText(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.cumIn, m.cumOut, m.cumCacheR, m.cumCacheW = 10, 5, 3, 7
	got := m.costText()
	if !strings.Contains(got, "in=10 out=5") || !strings.Contains(got, "read=3 write=7") {
		t.Fatalf("costText = %q", got)
	}
}

// TestCostTextShowsDollarEstimate — M9b: when the provider+model matches a
// known rate, /cost includes a "$…" line so the user sees the running spend.
func TestCostTextShowsDollarEstimate(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, &config.Config{Provider: "openai", Model: "gpt-4o"})
	m.cumIn = 1_000_000  // $2.50 at gpt-4o input
	m.cumOut = 1_000_000 // $10.00 at gpt-4o output
	got := m.costText()
	if !strings.Contains(got, "$") {
		t.Fatalf("costText missing $ estimate:\n%s", got)
	}
	if !strings.Contains(got, "12.5") {
		t.Fatalf("expected total ≈ $12.50, got:\n%s", got)
	}
}

// TestCostTextUnknownModelSkipsDollar — an unknown model produces no $ line
// (better than a bogus $0.00 that looks like a bug).
func TestCostTextUnknownModelSkipsDollar(t *testing.T) {
	m := newModel(&fakeControl{model: "unknown"}, &config.Config{Provider: "openai", Model: "unknown-model-xyz"})
	m.cumIn, m.cumOut = 100, 50
	if got := m.costText(); strings.Contains(got, "$") {
		t.Fatalf("unknown model should not show $, got:\n%s", got)
	}
}

// TestStatusLineShowsDollar — the fallback status line surfaces the running
// dollar cost alongside token counts so users see spend at a glance.
func TestStatusLineShowsDollar(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, &config.Config{Provider: "openai", Model: "gpt-4o"})
	m.cumIn, m.cumOut = 1_000_000, 500_000 // $2.50 + $5.00 = $7.50
	got := m.View()
	if !strings.Contains(got, "$") || !strings.Contains(got, "7.5") {
		t.Fatalf("status line missing $ estimate:\n%s", got)
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

// TestDoctorTextIncludesAgentscopeVersionAndMCP — M9a: /doctor should surface
// the linked agentscope-go version (from debug.ReadBuildInfo) and a live MCP
// server count so users can audit their runtime.
func TestDoctorTextIncludesAgentscopeVersionAndMCP(t *testing.T) {
	ctrl := &fakeControl{
		model: "gpt-4o",
		mcpServers: []mcpconfig.ServerInfo{
			{Name: "linear", ToolCount: 5},
			{Name: "github", ToolCount: 12},
		},
		jailed:        true,
		agentscopeVer: "v2.1.0-mock",
	}
	m := newModel(ctrl, &config.Config{
		Provider: "openai", Model: "gpt-4o", APIKey: "sk-x",
		Permission: "accept_edits",
	})
	got := m.doctorText()
	for _, want := range []string{"agentscope:", "v2.1.0-mock", "mcp:", "2 server", "jail:", "on"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/doctor missing %q:\n%s", want, got)
		}
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
