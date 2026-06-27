package agent

import (
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

func TestBuildSystemPrompt(t *testing.T) {
	tk := tool.NewEnhancedToolkit() // Bash/Read/Write/Edit/Glob/Grep
	p := buildSystemPrompt("/tmp/proj", tk, "FAKE PROJECT MEMORY")
	for _, want := range []string{"lathe", "Working directory: /tmp/proj", "Bash", "Read", "Write", "Edit", "Glob", "Grep", "FAKE PROJECT MEMORY"} {
		if !strings.Contains(p, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, p)
		}
	}
}

func TestBuildSystemPromptEmptyMemory(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	p := buildSystemPrompt("/tmp/proj", tk, "")
	if strings.Contains(p, "Project context") {
		t.Fatalf("empty memory should not add Project context section:\n%s", p)
	}
}
