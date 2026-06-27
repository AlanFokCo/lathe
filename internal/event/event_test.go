package event

import "testing"

func TestEventKinds(t *testing.T) {
	cases := []struct {
		ev   Event
		want string
	}{
		{TextDelta{Delta: "hi"}, "text_delta"},
		{ToolCallStart{ID: "t1", Name: "Read", Input: `{"path":"x"}`}, "tool_call_start"},
		{ToolResult{ID: "t1", Name: "Read", Output: "ok", State: "success"}, "tool_result"},
		{Usage{InputTokens: 3, OutputTokens: 2, Model: "gpt-4o"}, "usage"},
		{ReplyEnd{Reason: "end_turn"}, "reply_end"},
	}
	for _, c := range cases {
		if c.ev.Kind() != c.want {
			t.Fatalf("%#v: got %q want %q", c.ev, c.ev.Kind(), c.want)
		}
	}
}
