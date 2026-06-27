package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
)

// writeSkill writes a SKILL.md (YAML frontmatter name+description + body) into dir.
func writeSkill(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findByName(skills []skill.Skill, name string) (skill.Skill, bool) {
	for _, s := range skills {
		if s.Name == name {
			return s, true
		}
	}
	return skill.Skill{}, false
}

func TestDiscoverWalkUpAndDedup(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir() // repo root (has .git); sibling of home
	sub := filepath.Join(root, "sub")
	mustMkdir(t, sub)
	mustMkdir(t, filepath.Join(root, ".git"))

	// user-level
	writeSkill(t, filepath.Join(home, ".lathe", "skills", "onlyuser"), "onlyuser", "u", "ubody")
	writeSkill(t, filepath.Join(home, ".lathe", "skills", "shared"), "shared", "from-user", "userbody")
	// outer project (repo root)
	writeSkill(t, filepath.Join(root, ".lathe", "skills", "onlyouter"), "onlyouter", "o", "obody")
	writeSkill(t, filepath.Join(root, ".lathe", "skills", "shared"), "shared", "from-outer", "outerbody")
	// inner project (sub)
	writeSkill(t, filepath.Join(sub, ".lathe", "skills", "onlyinner"), "onlyinner", "i", "ibody")
	writeSkill(t, filepath.Join(sub, ".lathe", "skills", "shared"), "shared", "from-inner", "innerbody")

	t.Setenv("HOME", home)
	got, err := Discover(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 skills, got %d: %+v", len(got), got)
	}
	for _, name := range []string{"onlyuser", "onlyouter", "onlyinner", "shared"} {
		if _, ok := findByName(got, name); !ok {
			t.Fatalf("missing skill %q in %+v", name, got)
		}
	}
	sh, _ := findByName(got, "shared")
	if sh.Description != "from-inner" {
		t.Fatalf("shared desc: want from-inner (inner overrides), got %q", sh.Description)
	}
}

func TestDiscoverSubdirScan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	writeSkill(t, filepath.Join(root, ".lathe", "skills", "alpha", "beta"), "deep", "d", "body")
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findByName(got, "deep"); !ok {
		t.Fatalf("nested SKILL.md not found via ScanSubdir: %+v", got)
	}
}

func TestDiscoverMissingDirsNoError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "proj") // under home → walk stops at home (exclusive)
	mustMkdir(t, root)
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 skills when no SKILL.md exists, got %d: %+v", len(got), got)
	}
}

func TestDiscoverEmptyCwd(t *testing.T) {
	got, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty for empty cwd, got %+v", got)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
