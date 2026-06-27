package agent

import (
	"os"
	"path/filepath"
	"strings"
)

const maxMemoryBytes = 100 * 1024

// loadMemoryFiles collects CLAUDE.md and AGENTS.md from cwd up to the repo
// root (nearest directory containing .git), plus ~/.lathe/CLAUDE.md (user-level).
// Order: user-level first, then root→cwd (nearer files later). Missing files
// are skipped silently. Each file is capped at maxMemoryBytes.
func loadMemoryFiles(cwd string) string {
	var parts []string
	if home, err := os.UserHomeDir(); err == nil {
		if b := readMemFile(filepath.Join(home, ".lathe", "CLAUDE.md")); b != "" {
			parts = append(parts, b)
		}
	}
	// collect cwd→root, then emit root→cwd
	var levels []string
	dir := cwd
	for {
		levels = append(levels, dir)
		if isRepoRoot(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for i := len(levels) - 1; i >= 0; i-- {
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if b := readMemFile(filepath.Join(levels[i], name)); b != "" {
				parts = append(parts, b)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func readMemFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	if len(b) > maxMemoryBytes {
		b = append(b[:maxMemoryBytes], []byte("\n[truncated]\n")...)
	}
	return string(b)
}

func isRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
