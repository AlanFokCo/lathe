package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
)

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestSessionNewSaveLoad(t *testing.T) {
	setHome(t)
	sess, err := New("/Users/x/proj", "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SaveMeta(); err != nil {
		t.Fatal(err)
	}
	msgs := []*message.Msg{
		message.SystemMsg("s", "SYS"),
		message.UserMsg("u", "hi"),
		message.AssistantMsg("a", []message.ContentBlock{
			message.TextBlock{Type: "text", Text: "hello"},
			message.ToolCallBlock{Type: "tool_call", ID: "t1", Name: "Read", Input: `{"path":"x"}`},
		}),
	}
	for _, m := range msgs {
		if err := sess.Save(m); err != nil {
			t.Fatal(err)
		}
	}

	got, gotMsgs, err := Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sess.ID || got.Cwd != "/Users/x/proj" || got.Model != "gpt-4o" {
		t.Fatalf("metadata: %+v", got)
	}
	if len(gotMsgs) != len(msgs) {
		t.Fatalf("msg count: %d vs %d", len(gotMsgs), len(msgs))
	}
	// round-trip a tool_call block (polymorphic)
	asst := gotMsgs[2]
	tcs := asst.GetContentBlocks(message.ContentBlockToolCall)
	if len(tcs) != 1 {
		t.Fatalf("tool calls: %d", len(tcs))
	}
}

func TestSessionLatest(t *testing.T) {
	setHome(t)
	var paths []string
	for i := 0; i < 3; i++ {
		s, err := New("/p", "m")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveMeta(); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, s.Path)
	}
	mtime := time.Now()
	for i, p := range paths {
		_ = os.Chtimes(p, mtime.Add(time.Duration(i)*time.Second), mtime.Add(time.Duration(i)*time.Second))
	}
	got, _, err := Latest("/p")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != paths[2] {
		t.Fatalf("latest: got %s want %s", got.Path, paths[2])
	}
}

func TestSessionLoadCorruptLineSkipped(t *testing.T) {
	home := setHome(t)
	dir := filepath.Join(home, ".lathe", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bad.jsonl")
	meta := `{"id":"bad","cwd":"/p","model":"m","created_at":"x"}` + "\n"
	good, _ := json.Marshal(message.UserMsg("u", "ok"))
	if err := os.WriteFile(path, []byte(meta+string(good)+"\nGARBAGE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, msgs, err := Load("bad")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != "bad" {
		t.Fatalf("id: %s", sess.ID)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 good msg, got %d", len(msgs))
	}
}

func TestSessionLoadNotFound(t *testing.T) {
	setHome(t)
	if _, _, err := Load("nope"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestSessionLatestNone(t *testing.T) {
	setHome(t)
	if _, _, err := Latest("/empty-cwd"); err == nil {
		t.Fatal("expected no-sessions error")
	}
}

// TestSessionListNewestFirst — M6c-5: List returns summaries ordered by mtime
// (newest first), pulling model + first user prompt from each transcript so the
// /resume UI has enough to show without loading the full conversation.
func TestSessionListNewestFirst(t *testing.T) {
	setHome(t)
	var ids []string
	for i := 0; i < 3; i++ {
		s, err := New("/p", fmt.Sprintf("m%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveMeta(); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(message.UserMsg("u", fmt.Sprintf("prompt-%d", i))); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	// stagger mtimes so ordering is deterministic: ids[2] > ids[1] > ids[0]
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".lathe", "projects", "p")
	base := time.Now()
	for i, id := range ids {
		p := filepath.Join(dir, id+".jsonl")
		when := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	got := List("/p")
	if len(got) != 3 {
		t.Fatalf("want 3 summaries, got %d: %+v", len(got), got)
	}
	if got[0].ID != ids[2] || got[1].ID != ids[1] || got[2].ID != ids[0] {
		t.Fatalf("not newest-first: got %v want %v", []string{got[0].ID, got[1].ID, got[2].ID}, []string{ids[2], ids[1], ids[0]})
	}
	if got[0].Model != "m2" {
		t.Fatalf("model[0] = %q, want m2", got[0].Model)
	}
	if got[0].FirstPrompt != "prompt-2" {
		t.Fatalf("first prompt[0] = %q, want prompt-2", got[0].FirstPrompt)
	}
	if got[0].ModTime.IsZero() {
		t.Fatalf("mtime[0] should be set")
	}
}

// TestSessionListMissingDirEmpty — a cwd with no project dir is a valid empty
// result, not an error. /resume shows a friendly "no sessions" message.
func TestSessionListMissingDirEmpty(t *testing.T) {
	setHome(t)
	if got := List("/never-touched"); len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

// TestSessionListSkipsCorrupt — a file with unparseable metadata is skipped;
// well-formed siblings still show up.
func TestSessionListSkipsCorrupt(t *testing.T) {
	home := setHome(t)
	dir := filepath.Join(home, ".lathe", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// good session with a user prompt
	good, err := New("/p", "m-good")
	if err != nil {
		t.Fatal(err)
	}
	if err := good.SaveMeta(); err != nil {
		t.Fatal(err)
	}
	if err := good.Save(message.UserMsg("u", "hello")); err != nil {
		t.Fatal(err)
	}
	// corrupt: no valid metadata line
	if err := os.WriteFile(filepath.Join(dir, "bogus.jsonl"), []byte("GARBAGE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := List("/p")
	if len(got) != 1 || got[0].ID != good.ID {
		t.Fatalf("want only good session, got %+v", got)
	}
}
