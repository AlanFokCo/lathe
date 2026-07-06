package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// historyCap is the max number of entries kept in the persisted history file.
// When the file grows past this on Load, the oldest entries are dropped —
// bounded footprint without a full rewrite on every Append.
const historyCap = 1000

// history is the input-history ring used by the TUI for the ↑/↓ recall (M8b).
// Persists to disk on every Append so a crash mid-session does not lose recent
// prompts; skips blank + back-to-back duplicate entries per HISTCONTROL=ignoreboth.
type history struct {
	path    string
	entries []string
	browse  int // -1 not browsing; else index into entries pointing at last-returned Prev
}

// defaultHistoryPath is ~/.lathe/history when HOME resolves, else "".
func defaultHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lathe", "history")
}

// newHistory loads (or initializes) the history file at path.
func newHistory(path string) *history {
	h := &history{path: path, browse: -1}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		h.entries = append(h.entries, line)
	}
	if len(h.entries) > historyCap {
		h.entries = h.entries[len(h.entries)-historyCap:]
	}
	return h
}

// Append records entry (unless blank / dup of last) and persists to disk.
func (h *history) Append(entry string) {
	entry = strings.TrimRight(entry, "\n")
	if strings.TrimSpace(entry) == "" {
		return
	}
	if n := len(h.entries); n > 0 && h.entries[n-1] == entry {
		h.browse = -1
		return
	}
	h.entries = append(h.entries, entry)
	h.browse = -1
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(entry + "\n")
}

// Prev returns the previous entry (older direction). ok=false when already at
// the oldest entry. First call after ResetBrowse (or the initial state)
// returns the newest.
func (h *history) Prev() (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.browse < 0 {
		h.browse = len(h.entries) - 1
		return h.entries[h.browse], true
	}
	if h.browse == 0 {
		return h.entries[0], false // stay at oldest, signal boundary
	}
	h.browse--
	return h.entries[h.browse], true
}

// Next returns the entry after the currently-browsed one, or ("", true) when
// stepping past the newest (signals "clear the input"). ok=false only when
// not currently browsing at all.
func (h *history) Next() (string, bool) {
	if h.browse < 0 {
		return "", false
	}
	if h.browse >= len(h.entries)-1 {
		h.browse = -1
		return "", true
	}
	h.browse++
	return h.entries[h.browse], true
}

// ResetBrowse forgets the current browse position so the next Prev starts
// from the newest entry.
func (h *history) ResetBrowse() { h.browse = -1 }

// Browsing reports whether the user is currently mid-recall. Used by the TUI
// so successive ↑/↓ keep walking through history even after Prev populated
// the input with a value (which would otherwise fail the "empty input" guard).
func (h *history) Browsing() bool { return h.browse >= 0 }
