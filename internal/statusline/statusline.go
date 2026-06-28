// Package statusline renders lathe's TUI status line via a user-configured
// shell command (M5b). It mirrors Anthropic Claude Code's statusLine setting:
// a command receives a JSON snapshot on stdin and its stdout (trimmed, empty
// lines dropped) becomes the status line. M5b emits a claude-code-compatible
// JSON subset — fields lathe doesn't track yet are omitted, not stubbed.
package statusline

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Config is the parsed statusLine setting.
type Config struct {
	Type    string
	Command string
	Padding int
}

// Input is the lathe-side snapshot fed to BuildJSON/Run.
type Input struct {
	SessionID, TranscriptPath, Cwd, Model, Version string
	ContextSize, InputTokens, OutputTokens        int
}

// payload mirrors the claude-code-compatible subset. Field names match
// Claude Code's StatusLineCommandInput; omitted fields = lathe doesn't track.
type payload struct {
	SessionID      string              `json:"session_id"`
	TranscriptPath string              `json:"transcript_path"`
	Cwd            string              `json:"cwd"`
	Model          modelField          `json:"model"`
	Workspace      workspaceField      `json:"workspace"`
	Version        string              `json:"version"`
	ContextWindow  *contextWindowField `json:"context_window,omitempty"`
	Exceeds200k    bool                `json:"exceeds_200k_tokens"`
}

type modelField struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type workspaceField struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

type contextWindowField struct {
	TotalInputTokens    int     `json:"total_input_tokens"`
	TotalOutputTokens   int     `json:"total_output_tokens"`
	ContextWindowSize   int     `json:"context_window_size"`
	UsedPercentage      float64 `json:"used_percentage"`
	RemainingPercentage float64 `json:"remaining_percentage"`
}

// BuildJSON returns the claude-code-compatible subset JSON for in. The
// context_window object is omitted when ContextSize <= 0; exceeds_200k_tokens
// is always emitted (it only needs InputTokens).
func BuildJSON(in Input) []byte {
	p := payload{
		SessionID:      in.SessionID,
		TranscriptPath: in.TranscriptPath,
		Cwd:            in.Cwd,
		Model:          modelField{ID: in.Model, DisplayName: in.Model},
		Workspace:      workspaceField{CurrentDir: in.Cwd, ProjectDir: in.Cwd},
		Version:        in.Version,
		Exceeds200k:    in.InputTokens > 200000,
	}
	if in.ContextSize > 0 {
		used := float64(in.InputTokens) / float64(in.ContextSize) * 100
		if used > 100 {
			used = 100
		}
		remaining := 100 - used
		if remaining < 0 {
			remaining = 0
		}
		p.ContextWindow = &contextWindowField{
			TotalInputTokens:    in.InputTokens,
			TotalOutputTokens:   in.OutputTokens,
			ContextWindowSize:   in.ContextSize,
			UsedPercentage:      round1(used),
			RemainingPercentage: round1(remaining),
		}
	}
	b, _ := json.Marshal(p)
	return b
}

// round1 rounds f to 1 decimal place.
func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// Run executes cfg.Command with BuildJSON(in) on stdin. sh -c, cwd=in.Cwd,
// 5s timeout (or ctx deadline, whichever is sooner). exit 0 → stdout trimmed,
// split on '\n', empty lines dropped, each line trimmed, rejoined with '\n'
// (mirrors Claude Code's statusline stdout handling). Non-zero exit / timeout
// → "", err. Returns "", nil when cfg is not a command (Type != "command" or
// empty Command).
func Run(ctx context.Context, cfg Config, in Input) (string, error) {
	if cfg.Type != "command" || cfg.Command == "" {
		return "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cfg.Command)
	if in.Cwd != "" {
		cmd.Dir = in.Cwd
	}
	cmd.Stdin = strings.NewReader(string(BuildJSON(in)))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("statusline: timeout")
		}
		return "", fmt.Errorf("statusline: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return trimLines(stdout.String()), nil
}

// trimLines trims the whole, splits on newlines, drops empty lines, trims each,
// rejoins — matching Claude Code's statusline stdout normalization.
func trimLines(s string) string {
	out := []string{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
