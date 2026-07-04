package tui

import (
	"strings"
	"testing"
)

func TestScrollbackBuildsTextAndTool(t *testing.T) {
	var sb scrollback
	sb.appendUser("do X")
	sb.appendAssistantText("Hel")
	sb.appendAssistantText("lo")
	sb.appendTool("t1", "Read", `{"path":"x"}`)
	sb.appendToolResultDelta("t1", "line1\nline2\nline3")
	sb.finishTool("t1", "success")

	got := sb.build(80, -1)
	for _, want := range []string{"do X", "Hello", "Read", "●", "3 lines", "[✓]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("build missing %q:\n%s", want, got)
		}
	}
	// collapsed: full output must NOT be inlined
	if strings.Contains(got, "line1\nline2") {
		t.Fatalf("collapsed tool block leaked full output:\n%s", got)
	}
}

func TestScrollbackClear(t *testing.T) {
	var sb scrollback
	sb.appendUser("hi")
	sb.clear()
	if got := sb.build(80, -1); got != "" {
		t.Fatalf("expected empty after clear, got %q", got)
	}
}

func TestBuildFormatsAssistantOnBoundary(t *testing.T) {
	var sb scrollback
	sb.appendAssistantText("**hi**") // no newline → pending, raw
	if !strings.Contains(sb.build(80, -1), "**hi**") {
		t.Fatalf("mid-line should show raw pending: %q", sb.build(80, -1))
	}
	sb.appendAssistantText("\n") // boundary → commit + glamour
	if strings.Contains(sb.build(80, -1), "**") {
		t.Fatalf("post-boundary should be formatted (no **): %q", sb.build(80, -1))
	}
}

// TestAppendAssistantTextDoesNotClearCommitted — M5d iron rule 1 regression.
// The M5c-2 bug cleared formatted/commitLen on every delta, causing raw↔glamour
// oscillation. This test pins the fix.
func TestAppendAssistantTextDoesNotClearCommitted(t *testing.T) {
	var sb scrollback
	sb.appendAssistantText("abc\n")
	sb.build(80, -1)
	if sb.blocks[0].committed == "" {
		t.Fatalf("expected committed set after build: %+v", sb.blocks[0])
	}
	gotCommitLen := sb.blocks[0].commitLen
	sb.appendAssistantText("def") // must NOT clear committed / reset commitLen
	if sb.blocks[0].committed == "" {
		t.Fatalf("appendAssistantText cleared committed (regression): %+v", sb.blocks[0])
	}
	if sb.blocks[0].commitLen != gotCommitLen {
		t.Fatalf("appendAssistantText changed commitLen %d→%d (regression)", gotCommitLen, sb.blocks[0].commitLen)
	}
}

// TestStreamingCommittedMonotonic — M5d anti-flicker invariant: across
// token-by-token streaming, commitLen never decreases and committed, once
// non-empty, never reverts to empty.
func TestStreamingCommittedMonotonic(t *testing.T) {
	var sb scrollback
	tokens := []string{"```", "go\n", "foo\n", "bar", "\n", "```\n"}
	prevCommitLen := 0
	committedEverNonEmpty := false
	for _, tk := range tokens {
		sb.appendAssistantText(tk)
		sb.build(80, -1)
		b := sb.blocks[0]
		if b.commitLen < prevCommitLen {
			t.Fatalf("commitLen reverted %d→%d after %q", prevCommitLen, b.commitLen, tk)
		}
		prevCommitLen = b.commitLen
		if b.committed != "" {
			committedEverNonEmpty = true
		}
		if committedEverNonEmpty && b.committed == "" {
			t.Fatalf("committed reverted to empty after %q (oscillation)", tk)
		}
	}
}

func TestToolBlockStyled(t *testing.T) {
	var sb scrollback
	sb.appendTool("t1", "Bash", `{"command":"ls"}`)
	sb.appendToolResultDelta("t1", "done")
	sb.finishTool("t1", "success")
	got := sb.build(80, -1)
	if !strings.Contains(got, "● Bash") || !strings.Contains(got, "✓") || !strings.Contains(got, "done") {
		t.Fatalf("tool block styling missing:\n%s", got)
	}
}

// TestUsageNotRenderedInScrollback — M6a Commit B: usage (ModelCallEnd) updates
// cumulative tokens in the status line only; it is never added to the
// scrollback. Appending text + a usage event must not surface a [tokens] block.
func TestUsageNotRenderedInScrollback(t *testing.T) {
	var sb scrollback
	sb.appendUser("hi")
	if got := sb.build(80, -1); strings.Contains(got, "[tokens") {
		t.Fatalf("scrollback should not render a usage block:\n%s", got)
	}
}

func TestFinishAssistantMarksDone(t *testing.T) {
	var sb scrollback
	sb.appendAssistantText("hello\n")
	sb.build(80, -1)
	if sb.blocks[0].done {
		t.Fatal("block should be streaming before finishAssistant")
	}
	sb.finishAssistant()
	if !sb.blocks[0].done {
		t.Fatal("finishAssistant should mark block done")
	}
	// build of a done block renders full text via glamour cache
	if !strings.Contains(sb.build(80, -1), "hello") {
		t.Fatalf("done block lost text:\n%s", sb.build(80, -1))
	}
}

// TestBuildDoneBlockRendersFullTextNoTrailingNewline — M5d regression: a
// streamed assistant block whose text has no trailing newline has cl=0 while
// streaming. The streaming build must not poison committed with glamour's
// non-empty "" output, and the done build must render the full text.
func TestBuildDoneBlockRendersFullTextNoTrailingNewline(t *testing.T) {
	var sb scrollback
	sb.appendAssistantText("Hello") // no trailing newline → cl=0 while streaming
	sb.build(80, -1)                // streaming build
	sb.finishAssistant()
	got := sb.build(80, -1)
	if !strings.Contains(got, "Hello") {
		t.Fatalf("done block lost text (no trailing newline): %q", got)
	}
}
