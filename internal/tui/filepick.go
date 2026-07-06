package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// filePickerMax is the max entries shown in the @file picker panel (M9c). Kept
// small so the panel stays a compact line above the input; users refine with
// more characters when they need to disambiguate.
const filePickerMax = 8

// filePicker lists files under cwd and answers substring queries for the
// @file autocomplete panel. The list is cached on first use so navigating
// through matches does not re-scan the tree on every keystroke.
type filePicker struct {
	cwd    string
	cached bool
	list   []string // relative paths
}

func newFilePicker(cwd string) *filePicker { return &filePicker{cwd: cwd} }

// load populates the cached file list, preferring `git ls-files` (fast +
// honors .gitignore) with a fallback to a bounded walk that skips a few
// well-known noise dirs. Idempotent.
func (p *filePicker) load() {
	if p.cached || p.cwd == "" {
		return
	}
	p.cached = true
	if paths := gitLsFiles(p.cwd); paths != nil {
		p.list = paths
		return
	}
	p.list = walkFilesFallback(p.cwd)
}

// Match returns up to filePickerMax paths whose base name or full relative
// path contains query (case-insensitive). Base-name matches sort before
// full-path matches so typing "read" finds "README.md" before
// "cmd/reader/main.go". Empty query returns the first filePickerMax entries.
func (p *filePicker) Match(query string) []string {
	p.load()
	if len(p.list) == 0 {
		return nil
	}
	q := strings.ToLower(query)
	if q == "" {
		if len(p.list) > filePickerMax {
			return p.list[:filePickerMax]
		}
		return p.list
	}
	type hit struct {
		path string
		rank int
	}
	var hits []hit
	for _, pth := range p.list {
		lp := strings.ToLower(pth)
		base := strings.ToLower(filepath.Base(pth))
		switch {
		case strings.HasPrefix(base, q):
			hits = append(hits, hit{pth, 0})
		case strings.Contains(base, q):
			hits = append(hits, hit{pth, 1})
		case strings.Contains(lp, q):
			hits = append(hits, hit{pth, 2})
		}
		if len(hits) >= filePickerMax*4 { // small cap on inner loop cost
			break
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > filePickerMax {
		hits = hits[:filePickerMax]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.path
	}
	return out
}

// extractAtQuery parses the trailing @<query> token from input. Returns the
// query string (chars after the last @, up to end-of-input) and the prefix
// (input up to and including the @). ok=true only when a @ is present and no
// whitespace separates it from end-of-input. Query may be empty (bare @ →
// picker shows the first N files).
func extractAtQuery(input string) (query, prefix string, ok bool) {
	i := strings.LastIndex(input, "@")
	if i < 0 {
		return "", "", false
	}
	tail := input[i+1:]
	if strings.ContainsAny(tail, " \n\t") {
		return "", "", false
	}
	// require @ to be at start OR preceded by whitespace so we do not fire on
	// mid-word @ (e.g. an email address).
	if i > 0 {
		prev := input[i-1]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			return "", "", false
		}
	}
	return tail, input[:i+1], true
}

// gitLsFiles returns tracked files under root via `git ls-files`, or nil when
// the command fails (not a repo / no git binary). Paths are relative to root.
func gitLsFiles(root string) []string {
	cmd := exec.Command("git", "-C", root, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// walkFilesFallback is the no-git-available fallback: walks root, skips
// hidden dirs and a small set of noise dirs, caps at 5000 entries so a huge
// tree does not stall the TUI.
func walkFilesFallback(root string) []string {
	const cap = 5000
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "target": true, "dist": true, "build": true}
	var paths []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			base := d.Name()
			if skip[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= cap {
			return filepath.SkipAll
		}
		return nil
	})
	return paths
}
