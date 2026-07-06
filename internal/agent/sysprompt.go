package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

// buildSystemPrompt assembles the coding-agent system prompt: base role,
// environment (OS/cwd/git), tool catalogue, iron-law behavioural rules,
// project memory, and skills. M10a: claude-code-parity prompt engineering.
func buildSystemPrompt(cwd string, tk *tool.Toolkit, memory, skillsSection string) string {
	var b strings.Builder

	// ── identity ──
	b.WriteString("You are lathe, an interactive coding agent operating in a terminal.\n")
	b.WriteString("You help the user with software engineering tasks: reading, writing, and editing code, running commands, and searching the codebase.\n\n")

	// ── environment ──
	b.WriteString("# Environment\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Working directory: %s\n", cwd))
	if branch, dirty, ok := gitInfo(cwd); ok {
		b.WriteString(fmt.Sprintf("- Git: branch=%s dirty=%v\n", branch, dirty))
	}

	// ── tool catalogue ──
	b.WriteString("\n# Tools\n")
	for _, s := range tk.GetToolSchemas() {
		b.WriteString(fmt.Sprintf("- %s: %s\n", s.Function.Name, s.Function.Description))
	}

	// ── iron laws (M10a) ──
	b.WriteString(ironLaws)

	// ── project memory ──
	if strings.TrimSpace(memory) != "" {
		b.WriteString("\n# Project context (CLAUDE.md / AGENTS.md)\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}

	// ── skills ──
	if strings.TrimSpace(skillsSection) != "" {
		b.WriteString(skillsSection)
	}

	return b.String()
}

// ironLaws contains the behavioural rules injected into every system prompt.
// Kept as a const so tests can assert key phrases without building a full
// prompt, and so the rules are visible in one place. M10a.
const ironLaws = `
# Rules

## Parallel tool calls
When you intend to call multiple tools and there are no dependencies between them, issue all independent calls in the same assistant message. Maximize parallelism; only sequence calls that depend on a prior result.

## Task tracking (TodoWrite)
For any task with 3 or more steps, FIRST create a checklist via task_create before doing any work. Mark each task in_progress when you start it and completed as soon as it finishes — do not batch status updates.

## Read before write
You MUST Read a file before editing or overwriting it. Never write to a file you have not read in this conversation.

## Editing style
Prefer targeted edits (Edit, MultiEdit, ApplyPatch) over full rewrites. Only use Write for new files or when a complete rewrite is genuinely needed.

## Communication
- Be short, direct, and concise. No filler, no unnecessary thanks or apologies.
- Default to writing no code comments. Only add a comment when the WHY is non-obvious.
- Do not add backward-compatibility shims, unused re-exports, or "removed" markers for deleted code.
- When the task is done, give a one-sentence summary of what changed and what is next. Nothing more.

## Plan mode
When plan mode is active, do NOT modify files or run shell commands. Only read files, search code, and produce a plan for the user to approve.

## Skills
When available skills match the task, invoke the SkillViewer tool to read the skill instructions BEFORE starting work. Skills define specialised workflows — follow them.
`

// gitInfo returns (branch, dirty, ok); ok=false if cwd is not a git repo.
func gitInfo(cwd string) (branch string, dirty bool, ok bool) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", false, false
	}
	branch = strings.TrimSpace(string(out))
	stOut, err := exec.Command("git", "-C", cwd, "status", "--porcelain").Output()
	if err != nil {
		return branch, false, true
	}
	return branch, len(strings.TrimSpace(string(stOut))) > 0, true
}

// mustCwd returns the current working directory, or "" on error.
func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}
