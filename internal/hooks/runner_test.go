package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/alanfokco/lathe/internal/settings"
)

func TestRunnerBlock(t *testing.T) {
	r := NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "echo", Hooks: []settings.Command{{Type: "command", Command: `printf '{"decision":"block","reason":"no"}'`}}}},
	}, "/tmp", "")
	res, err := r.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Block || res.Reason != "no" {
		t.Fatalf("res: %+v", res)
	}
}

func TestRunnerContext(t *testing.T) {
	r := NewRunner(map[string][]settings.Matcher{
		"UserPromptSubmit": {{Hooks: []settings.Command{{Type: "command", Command: `printf '{"additionalContext":"X"}'`}}}},
	}, "/tmp", "")
	res, _ := r.Run(context.Background(), "UserPromptSubmit", map[string]any{"prompt": "hi"})
	if res.Context != "X" {
		t.Fatalf("context: %q", res.Context)
	}
}

func TestRunnerMatcher(t *testing.T) {
	r := NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "Write", Hooks: []settings.Command{{Type: "command", Command: `printf '{"decision":"block","reason":"w"}'`}}}},
	}, "/tmp", "")
	res, _ := r.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "echo"})
	if res.Block {
		t.Fatal("matcher Write should not match tool echo")
	}
	res, _ = r.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "Write"})
	if !res.Block {
		t.Fatal("matcher Write should match tool Write")
	}
}

func TestRunnerTimeoutNonBlocking(t *testing.T) {
	r := NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "*", Hooks: []settings.Command{{Type: "command", Command: "sleep 5", Timeout: 1}}}},
	}, "/tmp", "")
	start := time.Now()
	res, err := r.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "x"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if res.Block {
		t.Fatal("timeout should not block")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout did not fire: %v", elapsed)
	}
}

func TestRunnerNonZeroExitNonBlocking(t *testing.T) {
	r := NewRunner(map[string][]settings.Matcher{
		"PreToolUse": {{Matcher: "*", Hooks: []settings.Command{{Type: "command", Command: "exit 1"}}}},
	}, "/tmp", "")
	res, err := r.Run(context.Background(), "PreToolUse", map[string]any{"tool_name": "x"})
	if err != nil || res.Block {
		t.Fatalf("non-zero exit should be non-blocking: %+v %v", res, err)
	}
}

func TestRunnerNilSafe(t *testing.T) {
	var r *Runner
	res, err := r.Run(context.Background(), "PreToolUse", nil)
	if err != nil || res.Block || res.Context != "" {
		t.Fatalf("nil runner should return zero: %+v %v", res, err)
	}
}
