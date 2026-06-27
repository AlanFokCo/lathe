// Package skills discovers lathe's SKILL.md instruction documents from the
// user-level and project-level .lathe/skills directories and loads them via
// agentscope's skill package. Skills are instruction documents (not callable
// tools): the model reads a skill's body through the SkillViewerTool and
// follows it using the ordinary tools.
package skills

import (
	"os"
	"path/filepath"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
)

// Discover loads skills from ~/.lathe/skills (user-level) and from each
// .lathe/skills directory found walking from cwd up to the repo root (.git)
// or the user's home directory (whichever comes first; home itself is
// excluded from the project walk — it is handled as the user-level dir).
//
// Load order is user → outermost project → innermost project (cwd). On name
// collision the later-loaded skill wins: a project skill overrides a user
// skill of the same name, and an inner (closer to cwd) project skill
// overrides an outer one. Missing directories and unparseable SKILL.md files
// are skipped silently. Returns a nil slice (no error) when no skills exist.
func Discover(cwd string) ([]skill.Skill, error) {
	if cwd == "" {
		return nil, nil
	}
	var groups [][]skill.Skill

	if d := userSkillDir(); d != "" {
		if s, err := skill.NewLocalSkillLoader(d, true).LoadSkills(); err == nil {
			groups = append(groups, s)
		}
	}
	for _, d := range projectSkillDirs(cwd) {
		if s, err := skill.NewLocalSkillLoader(d, true).LoadSkills(); err == nil {
			groups = append(groups, s)
		}
	}

	// Dedup by Name, last-wins value, first-seen order preserved for stability.
	byName := make(map[string]skill.Skill)
	var order []string
	for _, grp := range groups {
		for _, s := range grp {
			if _, ok := byName[s.Name]; !ok {
				order = append(order, s.Name)
			}
			byName[s.Name] = s
		}
	}
	out := make([]skill.Skill, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out, nil
}

// userSkillDir returns ~/.lathe/skills, or "" if the home dir is unavailable.
func userSkillDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lathe", "skills")
}

// projectSkillDirs collects existing .lathe/skills directories from cwd up to
// the repo root (.git) or the user's home (exclusive), returned in root→cwd
// (outer→inner) order so callers loading in order get inner-wins dedup.
func projectSkillDirs(cwd string) []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	dir := cwd
	for {
		candidate := filepath.Join(dir, ".lathe", "skills")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			dirs = append(dirs, candidate)
		}
		if hasGit(dir) {
			break
		}
		if home != "" && dir == home {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// reverse collected (cwd→root) to root→cwd
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

// hasGit reports whether dir contains a .git entry (file or dir).
func hasGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
