package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

// buildSystemPrompt assembles the coding-agent system prompt: identity,
// environment (OS/cwd/git/file-tree), tool catalogue, behavioural rules,
// project memory, and skills. M12a: claude-code-parity overhaul.
func buildSystemPrompt(cwd string, tk *tool.Toolkit, memory, skillsSection string) string {
	var b strings.Builder

	b.WriteString(identity)

	// ── environment ──
	b.WriteString("\n# Environment\n")
	b.WriteString(fmt.Sprintf("- Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Working directory: %s\n", cwd))
	if branch, dirty, ok := gitInfo(cwd); ok {
		d := "clean"
		if dirty {
			d = "dirty"
		}
		b.WriteString(fmt.Sprintf("- Git: branch=%s (%s)\n", branch, d))
	}

	// ── file tree ──
	if tree := fileTree(cwd, 3, 80); tree != "" {
		b.WriteString("\n# Project structure\n```\n")
		b.WriteString(tree)
		b.WriteString("```\n")
	}

	// ── tool catalogue ──
	b.WriteString("\n# Available tools\n")
	for _, s := range tk.GetToolSchemas() {
		b.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Function.Name, firstSentence(s.Function.Description)))
	}

	// ── iron laws ──
	b.WriteString(ironLaws)

	// ── tool best practices ──
	b.WriteString(toolGuidance)

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

const identity = `You are lathe, an expert coding agent operating in a terminal. You assist the user with software engineering tasks: reading, writing, and editing code; running commands; debugging; refactoring; and answering questions about the codebase.

You are thorough, precise, and concise. You think carefully before acting, verify your work, and surface risks proactively. You match your response depth to the complexity of the question — a simple question gets a direct answer; a complex task gets a structured approach.
`

// ironLaws contains the core behavioural rules injected into every system
// prompt. M10a origin, M12a overhaul.
const ironLaws = `
# Rules

## Doing tasks
- For exploratory questions ("what could we do about X?"), respond in 2-3 sentences with a recommendation and the main tradeoff. Do not implement until the user agrees.
- For complex multi-step tasks, FIRST create a task list (task_create) before writing any code. Mark each task in_progress when you start and completed when you finish — do not batch.
- When you intend to call multiple tools with no dependencies between them, issue all calls in the same assistant turn. Maximize parallelism.
- After making changes, verify they work: run the build, run relevant tests, check for type errors. Never report "done" without verification.

## Reading and writing files
- You MUST Read a file before editing or overwriting it. Never write to a file you have not read in this conversation.
- Prefer targeted edits (Edit, MultiEdit, ApplyPatch) over full rewrites (Write). Only use Write for new files or when a complete rewrite is genuinely needed.
- When a file is too long to read at once, use the offset/limit parameters to read in chunks. Use Grep or Glob to locate the relevant section first.

## Code quality
- Do not add features, refactoring, or abstractions beyond what the task requires. A bug fix does not need surrounding cleanup; a one-shot operation does not need a helper.
- Default to writing no code comments. Only add a comment when the WHY is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug.
- Do not add error handling, fallbacks, or validation for scenarios that cannot happen. Trust internal code and framework guarantees. Only validate at system boundaries (user input, external APIs).
- Do not add backward-compatibility shims, unused re-exports, or "removed" markers for deleted code.
- Be careful not to introduce security vulnerabilities: command injection, XSS, SQL injection, path traversal. If you notice insecure code, fix it immediately.

## Communication
- Be short, direct, and concise. No filler, no unnecessary thanks or apologies.
- State what you are about to do in one sentence before your first tool call. Give short updates at key moments. End with a one-sentence summary of what changed.
- When referencing code, include the file path and line number so the user can navigate directly.
- Do not narrate your internal deliberation. State results and decisions directly.

## Git safety
- Never run destructive git commands (push --force, reset --hard, clean -f, branch -D) without explicit user confirmation.
- Never skip hooks (--no-verify) unless the user explicitly asks.
- Create new commits rather than amending existing ones, unless the user says otherwise.
- When staging files, prefer specific filenames over "git add -A" to avoid accidentally committing secrets or large binaries.

## Plan mode
When plan mode is active, do NOT modify files or run shell commands. Only read files, search code, and produce a plan for the user to approve.

## Skills
When available skills match the task, invoke the SkillViewer tool to read the skill instructions BEFORE starting work. Skills define specialised workflows — follow them.
`

// toolGuidance provides tool-specific best practices. M12a.
const toolGuidance = `
# Tool usage guidelines

## Bash
- Use for running builds, tests, git commands, and system operations — not for reading or writing files (use Read/Write/Edit instead).
- Always quote file paths with spaces. Use absolute paths when possible.
- For long-running commands, set a reasonable timeout.
- Never run interactive commands (editors, debuggers, REPLs) — they will hang.
- Prefer specific commands over broad ones: "go test ./internal/agent/ -run TestFoo" over "go test ./...".

## Read
- Read the whole file by default. Only use offset/limit for very large files (1000+ lines).
- Can read images (PNG, JPG) — contents are presented visually.
- Can read PDFs — use the pages parameter for large ones.

## Edit / MultiEdit
- The old_string must be unique in the file. If not, include more surrounding context to disambiguate.
- Preserve exact indentation from the file. The edit will fail on whitespace mismatch.
- Use MultiEdit when you need to make several changes to the same file in one call.

## Grep
- Use to find symbols, strings, or patterns across the codebase.
- Combine with Glob to narrow search scope.

## Task (subagent)
- Use for parallelizable subtasks: each subagent gets its own context and cannot see the parent conversation.
- Give the subagent enough context to work independently — include file paths, relevant background, and clear success criteria.
- The subagent has builtins only (Bash/Read/Write/Edit/Glob/Grep) — no MCP tools, no further Task recursion.

## LSP
- Use for navigating code: goToDefinition, findReferences, hover (type/docs), documentSymbol, workspaceSymbol.
- Requires a language server (gopls/tsserver/pylsp) — will fail gracefully if none is available for the file type.

## WebFetch
- Use to retrieve and analyze web content when the user provides a URL.
- The content is converted to markdown and may be summarized if very large.
- Never use for authenticated or private URLs.
`

// fileTree returns a shallow directory listing of root up to maxDepth levels,
// capped at maxEntries total entries. Skips noise directories (.git,
// node_modules, vendor, __pycache__, .next, dist, build, .cache, .venv,
// .tox, .mypy_cache, .pytest_cache, coverage, .DS_Store). M12a.
func fileTree(root string, maxDepth, maxEntries int) string {
	if root == "" {
		return ""
	}
	var lines []string
	count := 0
	truncated := false

	var walk func(dir string, prefix string, depth int)
	walk = func(dir string, prefix string, depth int) {
		if depth > maxDepth || truncated {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		// Sort: directories first, then files, both alphabetical.
		sort.Slice(entries, func(i, j int) bool {
			di, dj := entries[i].IsDir(), entries[j].IsDir()
			if di != dj {
				return di
			}
			return entries[i].Name() < entries[j].Name()
		})
		for _, e := range entries {
			if skipDir(e.Name()) {
				continue
			}
			if count >= maxEntries {
				truncated = true
				return
			}
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			lines = append(lines, prefix+name)
			count++
			if e.IsDir() {
				walk(filepath.Join(dir, e.Name()), prefix+"  ", depth+1)
			}
		}
	}
	walk(root, "", 0)
	if truncated {
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n") + "\n"
}

// skipDir returns true for directories/files that should be excluded from the
// file tree to reduce noise.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, ".vendor": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true,
	".next": true, ".nuxt": true, "dist": true, "build": true,
	".cache": true, ".venv": true, "venv": true, ".tox": true,
	"coverage": true, ".nyc_output": true, ".terraform": true,
	".DS_Store": true, "Thumbs.db": true,
	".idea": true, ".vscode": true,
	"target": true, "out": true,
}

func skipDir(name string) bool {
	return skipDirs[name]
}

// firstSentence returns the first sentence of s (up to the first period
// followed by a space or end-of-string), capped at 120 chars. Used to keep
// tool descriptions compact in the system prompt.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 && i < 120 {
		return s[:i+1]
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

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
