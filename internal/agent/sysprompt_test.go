package agent

import (
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

func TestBuildSystemPrompt(t *testing.T) {
	tk := tool.NewEnhancedToolkit() // Bash/Read/Write/Edit/Glob/Grep
	p := buildSystemPrompt("/tmp/proj", tk, "FAKE PROJECT MEMORY", "")
	for _, want := range []string{"lathe", "Working directory: /tmp/proj", "Bash", "Read", "Write", "Edit", "Glob", "Grep", "FAKE PROJECT MEMORY"} {
		if !strings.Contains(p, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, p)
		}
	}
}

func TestBuildSystemPromptEmptyMemory(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	p := buildSystemPrompt("/tmp/proj", tk, "", "")
	if strings.Contains(p, "Project context") {
		t.Fatalf("empty memory should not add Project context section:\n%s", p)
	}
}

func TestBuildSystemPromptWithSkills(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	section := "\n\n## Available Skills\n\n- **demo**: a demo skill\n"
	p := buildSystemPrompt("/tmp/proj", tk, "", section)
	if !strings.Contains(p, "Available Skills") || !strings.Contains(p, "demo") {
		t.Fatalf("skills section not appended:\n%s", p)
	}
}

func TestBuildSystemPromptEmptySkillsOmitsSection(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	p := buildSystemPrompt("/tmp/proj", tk, "", "")
	if strings.Contains(p, "Available Skills") {
		t.Fatalf("empty skillsSection should not add Available Skills:\n%s", p)
	}
}

// M10a: iron-law phrases that MUST appear in every system prompt.
func TestIronLawsPresent(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	p := buildSystemPrompt("/tmp/proj", tk, "", "")

	required := []struct {
		label  string
		phrase string
	}{
		{"parallel", "Parallel tool calls"},
		{"parallel-detail", "issue all independent calls in the same assistant message"},
		{"todowrite", "task_create"},
		{"todowrite-threshold", "3 or more steps"},
		{"todowrite-in_progress", "in_progress"},
		{"read-before-write", "Read a file before editing or overwriting"},
		{"edit-style", "Prefer targeted edits"},
		{"comms-short", "short, direct, and concise"},
		{"comms-no-comments", "no code comments"},
		{"comms-no-compat", "backward-compatibility"},
		{"plan-mode", "plan mode is active"},
		{"plan-mode-no-modify", "do NOT modify files"},
		{"skills-invoke", "SkillViewer"},
	}
	for _, tc := range required {
		if !strings.Contains(p, tc.phrase) {
			t.Errorf("[%s] system prompt missing phrase %q", tc.label, tc.phrase)
		}
	}
}

// M10a: ironLaws const must be non-empty and contain core section headers.
func TestIronLawsConst(t *testing.T) {
	if len(ironLaws) < 100 {
		t.Fatal("ironLaws const is suspiciously short")
	}
	for _, header := range []string{"# Rules", "## Parallel", "## Task tracking", "## Read before write", "## Communication", "## Plan mode", "## Skills"} {
		if !strings.Contains(ironLaws, header) {
			t.Errorf("ironLaws missing section header %q", header)
		}
	}
}
