package tui

import "testing"

func TestInsideOpenFence(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", false},
		{"plain text\n", false},
		{"```\n", true},
		{"```\ncode\n", true},
		{"```\ncode\n```\n", false},
		{"~~~\nfoo\n", true},
		{"```\ncode\n```\n\nprose\n", false},
		{"  ```\n", true},        // leading-space fence (common)
		{"```\n```\n```\n", true}, // odd count → open
	}
	for _, c := range cases {
		if got := insideOpenFence(c.text); got != c.want {
			t.Fatalf("insideOpenFence(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestCommitLenFor(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"abc", 0},            // no newline, not in fence → all pending
		{"abc\n", 4},          // after last newline
		{"abc\ndef", 4},       // partial last line stays pending
		{"abc\ndef\n", 8},
		{"```\n", 4},           // in open fence → len
		{"```\nfoo\n", 8},      // in open fence → len (3+1+3+1 = 8)
		{"```\nfoo\n```\n", 12}, // fence closed → after last newline (12 == len)
	}
	for _, c := range cases {
		if got := commitLenFor(c.text); got != c.want {
			t.Fatalf("commitLenFor(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name, output, state, diff, want string
	}{
		{"Read", "line1\nline2\nline3", "success", "", "→ 3 lines"},
		{"Read", "one line", "success", "", "→ 8 chars"},
		{"Read", "", "success", "", "→ (no output)"},
		{"Bash", "ok 3.2s\nmore", "success", "", "ok 3.2s"},
		{"Bash", "", "error", "", "→ exit"},
		{"Edit", "...", "success", "@@ \n+added\n-removed\n+added2\n", "edited +2 -1"},
		{"Write", "wrote 42 bytes", "success", "", "→ wrote 42 bytes"},
		{"Other", "", "success", "", "→ (no output)"},
	}
	for _, c := range cases {
		if got := summarize(c.name, c.output, c.state, c.diff); got != c.want {
			t.Fatalf("summarize(%q,%q,%q,%q) = %q, want %q", c.name, c.output, c.state, c.diff, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("truncate short: %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("truncate long: %q", got)
	}
	// rune-safe for CJK
	if got := truncate("你好世界你好世界", 4); got != "你好世界…" {
		t.Fatalf("truncate CJK: %q", got)
	}
}

func TestWrapRaw(t *testing.T) {
	got := wrapRaw("the quick brown fox jumps over the lazy dog", 10)
	// must not contain any line longer than 10
	for _, line := range splitLines(got) {
		if len(line) > 10 {
			t.Fatalf("wrapRaw line too long (%d): %q", len(line), line)
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
