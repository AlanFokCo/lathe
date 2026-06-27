package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMemoryFilesWalkUpAndUser(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	sub2 := filepath.Join(sub, "sub2")
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, c string) {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, ".git"), "stub") // mark repo root
	write(filepath.Join(root, "CLAUDE.md"), "ROOT")
	write(filepath.Join(sub, "AGENTS.md"), "MID")
	write(filepath.Join(sub2, "CLAUDE.md"), "LEAF")

	// user-level ~/.lathe/CLAUDE.md
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".lathe"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(home, ".lathe", "CLAUDE.md"), "USER")
	t.Setenv("HOME", home)

	got := loadMemoryFiles(sub2)
	// expected order: USER, ROOT, MID, LEAF
	order := []string{"USER", "ROOT", "MID", "LEAF"}
	prev := -1
	for _, m := range order {
		i := strings.Index(got, m)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", m, got)
		}
		if i <= prev {
			t.Fatalf("%q at %d not after prev %d:\n%s", m, i, prev, got)
		}
		prev = i
	}
}

func TestLoadMemoryFilesEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if got := loadMemoryFiles(root); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
