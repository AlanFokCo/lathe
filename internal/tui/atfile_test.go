package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpandAtFilesInlinesText — M7d.1: a bare @path pointing at a text file
// under cwd is replaced with a marker-wrapped inline copy so the model can
// reason over the file without having to make a Read tool call.
func TestExpandAtFilesInlinesText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtFiles("summarize @notes.md please", dir)
	if !strings.Contains(got, "hello world") {
		t.Fatalf("expansion missing content:\n%s", got)
	}
	if !strings.Contains(got, "notes.md") {
		t.Fatalf("expansion missing path marker:\n%s", got)
	}
	if !strings.Contains(got, "summarize") || !strings.Contains(got, "please") {
		t.Fatalf("expansion dropped surrounding text:\n%s", got)
	}
}

// TestExpandAtFilesLeavesMissingVerbatim — a @path that does not resolve to a
// real file stays as-is (models sometimes use @ for at-mentions unrelated to
// files; silent expansion would be surprising).
func TestExpandAtFilesLeavesMissingVerbatim(t *testing.T) {
	dir := t.TempDir()
	got := expandAtFiles("ping @nobody see @never.md", dir)
	if got != "ping @nobody see @never.md" {
		t.Fatalf("missing files should stay verbatim, got %q", got)
	}
}

// TestExpandAtFilesSkipsBinary — a JPEG-looking file is left alone; multimodal
// inlining is a future story. The @token stays as a hint the user probably
// meant to attach an image.
func TestExpandAtFilesSkipsBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.jpg")
	// PNG header — clearly binary
	if err := os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtFiles("what is in @pic.jpg", dir)
	if strings.Contains(got, "BEGIN FILE") {
		t.Fatalf("binary should not be inlined:\n%s", got)
	}
	if !strings.Contains(got, "@pic.jpg") {
		t.Fatalf("binary @path should stay verbatim:\n%s", got)
	}
}

// TestExpandAtFilesSkipsLargeFile — files over the size cap are skipped so a
// user does not accidentally blow the context window with @huge.log.
func TestExpandAtFilesSkipsLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	big := make([]byte, atFileMaxBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtFiles("look at @huge.txt", dir)
	if strings.Contains(got, "BEGIN FILE") {
		t.Fatalf("large file should not be inlined")
	}
}

// TestExpandAtFilesRejectsPathEscape — refuses paths that would resolve
// outside cwd (like ../../etc/passwd) so the model can not trick a user into
// leaking secrets by asking the user to type a token.
func TestExpandAtFilesRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	got := expandAtFiles("read @../etc/passwd", dir)
	if strings.Contains(got, "BEGIN FILE") {
		t.Fatalf("path escape should not be inlined:\n%s", got)
	}
}
