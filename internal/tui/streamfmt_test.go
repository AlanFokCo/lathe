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
