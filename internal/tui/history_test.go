package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHistoryAppendPersistsAndReloads — M8b: Append writes to disk immediately
// so a crash mid-session does not lose recent prompts, and a fresh Load reads
// them back in the same order.
func TestHistoryAppendPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := newHistory(path)
	h.Append("first")
	h.Append("second")
	h.Append("third")

	got := loadHistoryEntries(path)
	if len(got) != 3 || got[0] != "first" || got[2] != "third" {
		t.Fatalf("reload = %v", got)
	}

	h2 := newHistory(path)
	if len(h2.entries) != 3 {
		t.Fatalf("newHistory did not load existing file: %+v", h2.entries)
	}
}

// TestHistorySkipsBlankAndDuplicate — blank prompts and back-to-back dup
// prompts are not persisted (matches bash/zsh HISTCONTROL=ignoreboth).
func TestHistorySkipsBlankAndDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := newHistory(path)
	h.Append("")
	h.Append("  ")
	h.Append("do X")
	h.Append("do X") // duplicate of previous → skipped
	h.Append("do Y")
	if len(h.entries) != 2 {
		t.Fatalf("entries: %+v", h.entries)
	}
}

// TestHistoryPrevNextCycles — Prev walks back through entries, Next forward,
// End (returning "") when past the newest.
func TestHistoryPrevNextCycles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := newHistory(path)
	h.Append("one")
	h.Append("two")
	h.Append("three")

	if got, _ := h.Prev(); got != "three" {
		t.Fatalf("Prev 1 = %q, want three", got)
	}
	if got, _ := h.Prev(); got != "two" {
		t.Fatalf("Prev 2 = %q, want two", got)
	}
	if got, _ := h.Prev(); got != "one" {
		t.Fatalf("Prev 3 = %q, want one", got)
	}
	if _, ok := h.Prev(); ok {
		t.Fatal("Prev past oldest should return ok=false")
	}
	if got, _ := h.Next(); got != "two" {
		t.Fatalf("Next 1 = %q, want two", got)
	}
	if got, _ := h.Next(); got != "three" {
		t.Fatalf("Next 2 = %q, want three", got)
	}
	if got, ok := h.Next(); got != "" || !ok {
		t.Fatalf("Next past newest should return (\"\", true), got (%q, %v)", got, ok)
	}
}

// TestHistoryResetBrowse — a fresh keystroke path invokes ResetBrowse so the
// next Prev starts from the newest entry again.
func TestHistoryResetBrowse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := newHistory(path)
	h.Append("a")
	h.Append("b")
	h.Prev()
	h.Prev()
	h.ResetBrowse()
	if got, _ := h.Prev(); got != "b" {
		t.Fatalf("Prev after reset = %q, want b", got)
	}
}

func loadHistoryEntries(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range splitLinesTest(string(data)) {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func splitLinesTest(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
