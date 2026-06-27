package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
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
