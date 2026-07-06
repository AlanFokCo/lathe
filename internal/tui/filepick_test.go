package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFilePickerMatchesFallbackWalk — outside a git repo, walkFilesFallback
// still discovers files under cwd for the picker.
func TestFilePickerMatchesFallbackWalk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "hi")
	writeFile(t, filepath.Join(dir, "src", "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "node_modules", "junk.js"), "// skipped") // hidden by skip list
	p := newFilePicker(dir)
	got := p.Match("read")
	if len(got) == 0 || got[0] != "README.md" {
		t.Fatalf("expected README.md first, got %v", got)
	}
	got = p.Match("main")
	if len(got) == 0 || got[0] != "src/main.go" {
		t.Fatalf("expected src/main.go, got %v", got)
	}
	// noise dirs are skipped
	got = p.Match("junk")
	if len(got) != 0 {
		t.Fatalf("node_modules should be skipped, got %v", got)
	}
}

// TestFilePickerCap — the picker caps at filePickerMax so a huge tree does
// not overwhelm the panel.
func TestFilePickerCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < filePickerMax+5; i++ {
		writeFile(t, filepath.Join(dir, "file"+string(rune('a'+i))+".txt"), "")
	}
	p := newFilePicker(dir)
	got := p.Match("file")
	if len(got) != filePickerMax {
		t.Fatalf("got %d matches, want %d", len(got), filePickerMax)
	}
}

// TestExtractAtQueryHappyPath — trailing @token is picked up as the query.
func TestExtractAtQueryHappyPath(t *testing.T) {
	q, prefix, ok := extractAtQuery("read @rea")
	if !ok || q != "rea" || prefix != "read @" {
		t.Fatalf("got q=%q prefix=%q ok=%v", q, prefix, ok)
	}
}

// TestExtractAtQueryBareAt — bare @ (no query yet) still triggers the picker.
func TestExtractAtQueryBareAt(t *testing.T) {
	q, prefix, ok := extractAtQuery("look at @")
	if !ok || q != "" || prefix != "look at @" {
		t.Fatalf("bare @: q=%q prefix=%q ok=%v", q, prefix, ok)
	}
}

// TestExtractAtQueryMidWordIgnored — @ mid-word (e.g. email) does not
// trigger the picker, keeping the panel out of prompts about addresses.
func TestExtractAtQueryMidWordIgnored(t *testing.T) {
	if _, _, ok := extractAtQuery("mail me at alan@example.com"); ok {
		t.Fatal("mid-word @ should not trigger picker (test 1)")
	}
	if _, _, ok := extractAtQuery("no at here"); ok {
		t.Fatal("no @ should not trigger")
	}
	if _, _, ok := extractAtQuery("trailing @ path/foo"); ok {
		t.Fatal("@ followed by space should not trigger (query would be empty and followed by content)")
	}
}
