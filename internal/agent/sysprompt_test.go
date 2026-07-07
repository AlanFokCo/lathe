package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

func TestBuildSystemPrompt(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
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

// M10a+M12a: phrases that MUST appear in every system prompt.
func TestIronLawsPresent(t *testing.T) {
	tk := tool.NewEnhancedToolkit()
	p := buildSystemPrompt("/tmp/proj", tk, "", "")

	required := []struct {
		label  string
		phrase string
	}{
		{"parallel", "Maximize parallelism"},
		{"todowrite", "task_create"},
		{"todowrite-threshold", "multi-step"},
		{"read-before-write", "Read a file before editing or overwriting"},
		{"edit-style", "Prefer targeted edits"},
		{"comms-short", "short, direct, and concise"},
		{"comms-no-comments", "no code comments"},
		{"comms-no-compat", "backward-compatibility"},
		{"plan-mode", "plan mode is active"},
		{"plan-mode-no-modify", "do NOT modify files"},
		{"skills-invoke", "SkillViewer"},
		{"security", "security vulnerabilities"},
		{"git-safety", "destructive git commands"},
		{"verify", "verify"},
	}
	for _, tc := range required {
		if !strings.Contains(p, tc.phrase) {
			t.Errorf("[%s] system prompt missing phrase %q", tc.label, tc.phrase)
		}
	}
}

// M10a+M12a: ironLaws + toolGuidance must be non-empty and contain sections.
func TestIronLawsConst(t *testing.T) {
	if len(ironLaws) < 100 {
		t.Fatal("ironLaws const is suspiciously short")
	}
	for _, header := range []string{"# Rules", "## Doing tasks", "## Reading and writing", "## Code quality", "## Communication", "## Git safety", "## Plan mode", "## Skills"} {
		if !strings.Contains(ironLaws, header) {
			t.Errorf("ironLaws missing section header %q", header)
		}
	}
}

func TestToolGuidanceConst(t *testing.T) {
	if len(toolGuidance) < 100 {
		t.Fatal("toolGuidance const is suspiciously short")
	}
	for _, section := range []string{"## Bash", "## Read", "## Edit", "## Grep", "## Task", "## LSP", "## WebFetch"} {
		if !strings.Contains(toolGuidance, section) {
			t.Errorf("toolGuidance missing section %q", section)
		}
	}
}

// M12a: fileTree tests.
func TestFileTreeBasic(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("package src"), 0o644)

	tree := fileTree(dir, 3, 100)
	if !strings.Contains(tree, "src/") {
		t.Fatalf("tree missing src/:\n%s", tree)
	}
	if !strings.Contains(tree, "go.mod") {
		t.Fatalf("tree missing go.mod:\n%s", tree)
	}
	if !strings.Contains(tree, "main.go") {
		t.Fatalf("tree missing main.go:\n%s", tree)
	}
	if !strings.Contains(tree, "app.go") {
		t.Fatalf("tree missing app.go:\n%s", tree)
	}
}

func TestFileTreeSkipsNoiseDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "foo"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte(""), 0o644)

	tree := fileTree(dir, 3, 100)
	if strings.Contains(tree, "node_modules") {
		t.Fatalf("tree should skip node_modules:\n%s", tree)
	}
	if strings.Contains(tree, ".git") {
		t.Fatalf("tree should skip .git:\n%s", tree)
	}
	if !strings.Contains(tree, "src/") {
		t.Fatalf("tree should include src/:\n%s", tree)
	}
}

func TestFileTreeTruncates(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.txt", i)), []byte(""), 0o644)
	}
	tree := fileTree(dir, 1, 5)
	if !strings.Contains(tree, "... (truncated)") {
		t.Fatalf("expected truncation marker:\n%s", tree)
	}
}

func TestFileTreeDepthLimit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "d", "deep.txt"), []byte(""), 0o644)

	tree := fileTree(dir, 2, 100)
	if strings.Contains(tree, "deep.txt") {
		t.Fatalf("depth=2 should not reach depth-4 file:\n%s", tree)
	}
}

func TestFileTreeEmptyDir(t *testing.T) {
	tree := fileTree("", 3, 100)
	if tree != "" {
		t.Fatalf("empty root should return empty string")
	}
}

// M12a: firstSentence tests.
func TestFirstSentence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello world. More text here.", "Hello world."},
		{"Short", "Short"},
		{strings.Repeat("x", 200), strings.Repeat("x", 117) + "..."},
		{"", ""},
	}
	for _, tc := range cases {
		got := firstSentence(tc.in)
		if got != tc.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.in[:min(len(tc.in), 20)], got, tc.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
