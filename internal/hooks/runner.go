// Package hooks runs user-configured shell-command hooks at tool/turn
// boundaries. A hook receives a JSON object on stdin and may return a JSON
// object on stdout (e.g. {"decision":"block","reason":"..."} to deny a tool,
// or {"additionalContext":"..."} to inject context). Failures are non-blocking.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alanfokco/lathe/internal/settings"
)

// Runner executes hooks from a settings.Hooks config. A nil *Runner is safe
// to call Run on (returns zero Result) so callers need no nil checks.
type Runner struct {
	hooks     map[string][]settings.Matcher
	cwd       string
	sessionID string
}

// Result is the aggregated outcome of running the matching hooks for an event.
type Result struct {
	Block   bool
	Reason  string
	Context string
}

// NewRunner builds a Runner from a hooks config. The runner is non-nil even
// with empty hooks; Run on a nil *Runner is also safe.
func NewRunner(hooks map[string][]settings.Matcher, cwd, sessionID string) *Runner {
	return &Runner{hooks: hooks, cwd: cwd, sessionID: sessionID}
}

// Run executes all hooks matching the event (and tool_name in payload, for
// Pre/PostToolUse). It never returns an error — per-hook failures are warned
// to stderr and skipped (non-blocking). Nil receiver → zero Result.
func (r *Runner) Run(ctx context.Context, event string, payload map[string]any) (Result, error) {
	if r == nil || r.hooks == nil {
		return Result{}, nil
	}
	matchers, ok := r.hooks[event]
	if !ok {
		return Result{}, nil
	}
	toolName, _ := payload["tool_name"].(string)
	out := Result{}
	for _, m := range matchers {
		if !matchesTool(m.Matcher, event, toolName) {
			continue
		}
		for _, h := range m.Hooks {
			res, err := r.runOne(ctx, event, h, payload)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hook", event, "error:", err)
				continue
			}
			if res.Block {
				out.Block = true
				if out.Reason == "" {
					out.Reason = res.Reason
				}
			}
			if res.Context != "" {
				if out.Context != "" {
					out.Context += "\n"
				}
				out.Context += res.Context
			}
		}
	}
	return out, nil
}

// matchesTool reports whether a matcher applies. UserPromptSubmit/Stop have
// no tool name and always match; otherwise match "", "*", or exact tool name.
func matchesTool(matcher, event, toolName string) bool {
	switch event {
	case "UserPromptSubmit", "Stop":
		return true
	default:
		return matcher == "" || matcher == "*" || matcher == toolName
	}
}

// runOne executes one hook command and parses its stdout JSON.
func (r *Runner) runOne(ctx context.Context, event string, h settings.Command, payload map[string]any) (Result, error) {
	if h.Type != "command" {
		return Result{}, nil
	}
	timeout := time.Duration(h.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "sh", "-c", h.Command)
	cmd.Dir = r.cwd

	stdin := map[string]any{"hook_event_name": event}
	for k, v := range payload {
		stdin[k] = v
	}
	stdin["cwd"] = r.cwd
	stdin["session_id"] = r.sessionID
	stdinBytes, _ := json.Marshal(stdin)
	cmd.Stdin = strings.NewReader(string(stdinBytes))

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("timeout")
		}
		return Result{}, fmt.Errorf("exit: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	var parsed struct {
		Decision          string `json:"decision"`
		Reason            string `json:"reason"`
		AdditionalContext string `json:"additionalContext"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &parsed); err != nil {
		return Result{}, nil // non-JSON stdout → non-blocking, no result
	}
	res := Result{Reason: parsed.Reason, Context: parsed.AdditionalContext}
	if parsed.Decision == "block" {
		res.Block = true
	}
	return res, nil
}
