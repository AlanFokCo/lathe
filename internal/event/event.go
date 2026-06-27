// Package event defines lathe's event stream: the turn engine emits these,
// and the render layer (and M2 TUI) consumes them.
package event

// Event is the sealed interface for all turn-engine events.
type Event interface{ Kind() string }

// TextDelta is a streamed text fragment from the model.
type TextDelta struct{ Delta string }

func (TextDelta) Kind() string { return "text_delta" }

// ToolCallStart signals the model requested a tool call.
type ToolCallStart struct {
	ID, Name, Input string
}

func (ToolCallStart) Kind() string { return "tool_call_start" }

// ToolResult signals a tool finished (with its output/state, plus any diff).
type ToolResult struct {
	ID, Name string
	Output   string
	State    string // a message.ToolResultState value: success/error/denied/...
	Diff     string
}

func (ToolResult) Kind() string { return "tool_result" }

// Usage reports token consumption for a model call.
type Usage struct {
	InputTokens, OutputTokens int
	Model                     string
}

func (Usage) Kind() string { return "usage" }

// ReplyEnd signals the turn finished. Reason is one of:
// end_turn | max_iters | cancelled | error.
type ReplyEnd struct{ Reason string }

func (ReplyEnd) Kind() string { return "reply_end" }

// ErrorEvent signals a fatal error during the turn.
type ErrorEvent struct{ Err error }

func (ErrorEvent) Kind() string { return "error" }

// Compacted signals the conversation was auto-compressed (before→after tokens).
type Compacted struct{ Before, After int }

func (Compacted) Kind() string { return "compacted" }
