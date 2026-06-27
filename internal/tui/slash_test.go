package tui

import "testing"

func TestParseSlash(t *testing.T) {
	cases := []struct {
		in       string
		wantCmd  string
		wantRest string
		wantOk   bool
	}{
		{"/help", "help", "", true},
		{"/clear", "clear", "", true},
		{"/quit", "quit", "", true},
		{"/model sonnet-4", "model", "sonnet-4", true},
		{"hello", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		cmd, rest, ok := parseSlash(c.in)
		if cmd != c.wantCmd || rest != c.wantRest || ok != c.wantOk {
			t.Fatalf("parseSlash(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, cmd, rest, ok, c.wantCmd, c.wantRest, c.wantOk)
		}
	}
}
