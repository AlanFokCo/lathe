package agent

import (
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
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
