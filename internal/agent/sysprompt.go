package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// buildSystemPrompt assembles the coding-agent system prompt: base role,
// environment (OS/cwd/git), tool descriptions, project memory, and skills.
func buildSystemPrompt(cwd string, tk *tool.Toolkit, memory, skillsSection string) string {
	var b strings.Builder
	b.WriteString("You are lathe, an interactive coding agent operating in a terminal.\n")
	b.WriteString("You help the user with software engineering tasks: reading, writing, and editing code, running commands, and searching the codebase.\n\n")

	b.WriteString("Environment:\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Working directory: %s\n", cwd))
	if branch, dirty, ok := gitInfo(cwd); ok {
		b.WriteString(fmt.Sprintf("- Git: branch=%s dirty=%v\n", branch, dirty))
	}

	b.WriteString("\nTools:\n")
	for _, s := range tk.GetToolSchemas() {
		b.WriteString(fmt.Sprintf("- %s: %s\n", s.Function.Name, s.Function.Description))
	}

	if strings.TrimSpace(memory) != "" {
		b.WriteString("\n# Project context (CLAUDE.md / AGENTS.md)\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}

	if strings.TrimSpace(skillsSection) != "" {
		b.WriteString(skillsSection)
	}

	b.WriteString("\nPrefer targeted edits over full rewrites. Run commands to verify your work. When the task is complete, give a concise summary of what you did.")
	return b.String()
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
