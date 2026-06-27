package agent

import (
	"strings"
	"testing"
)

func TestSystemPromptContainsRequiredSections(t *testing.T) {
	p := systemPrompt()
	for _, want := range []string{"lathe", "Working directory", "Bash", "Read", "Write", "Edit", "Glob", "Grep"} {
		if !strings.Contains(p, want) {
			t.Fatalf("system prompt missing %q\n%s", want, p)
		}
	}
}
