// Package workspace builds a sandboxed tool toolkit that routes Bash/Read/
// Write/Edit/Glob/Grep through an agentscope workspace.Workspace (e.g. a
// Docker container or E2B cloud sandbox). Used when lathe runs with
// --sandbox; without it, lathe uses the host agentscope builtins.
package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/workspace"
)

// WorkspaceToolkit returns a Toolkit of Bash/Read/Write/Edit/Glob/Grep tools
// that route through the given Workspace.
func WorkspaceToolkit(ws workspace.Workspace) *tool.Toolkit {
	return tool.NewToolkit(
		bashTool(ws),
		readTool(ws),
		writeTool(ws),
		editTool(ws),
		globTool(ws),
		grepTool(ws),
	)
}

func bashTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Bash", "Execute a shell command in the workspace.",
		json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command"}},"required":["command"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			cmd, _ := input["command"].(string)
			if cmd == "" {
				return nil, fmt.Errorf("command is required")
			}
			res, err := ws.Execute(ctx, cmd)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"exit_code": res.ExitCode, "output": res.Stdout}
			if res.Stderr != "" {
				out["error"] = res.Stderr
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		},
	)
}

func readTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Read", "Read a file from the workspace.",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"}},"required":["path"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}
			data, err := ws.ReadFile(ctx, path)
			if err != nil {
				return nil, err
			}
			return string(data), nil
		},
	)
}

func writeTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Write", "Write a file in the workspace.",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)
			content, _ := input["content"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}
			if err := ws.WriteFile(ctx, path, []byte(content)); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	)
}

func editTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Edit", "Edit a file by replacing old_string with new_string (must be unique).",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)
			oldS, _ := input["old_string"].(string)
			newS, _ := input["new_string"].(string)
			if path == "" || oldS == "" {
				return nil, fmt.Errorf("path and old_string are required")
			}
			data, err := ws.ReadFile(ctx, path)
			if err != nil {
				return nil, err
			}
			content := string(data)
			count := strings.Count(content, oldS)
			if count == 0 {
				return nil, fmt.Errorf("old_string not found in %s", path)
			}
			if count > 1 {
				return nil, fmt.Errorf("old_string appears %d times in %s (must be unique)", count, path)
			}
			newContent := strings.Replace(content, oldS, newS, 1)
			if err := ws.WriteFile(ctx, path, []byte(newContent)); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	)
}

func globTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Glob", "Find files matching a glob pattern.",
		json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			pattern, _ := input["pattern"].(string)
			if pattern == "" {
				return nil, fmt.Errorf("pattern is required")
			}
			cmd := fmt.Sprintf("find %s -type f -name '%s'", ws.BasePath(), shellQuote(pattern))
			res, err := ws.Execute(ctx, cmd)
			if err != nil {
				return nil, err
			}
			return res.Stdout, nil
		},
	)
}

func grepTool(ws workspace.Workspace) tool.Tool {
	return tool.NewFunctionTool("Grep", "Search file contents for a pattern.",
		json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			pattern, _ := input["pattern"].(string)
			path, _ := input["path"].(string)
			if pattern == "" {
				return nil, fmt.Errorf("pattern is required")
			}
			if path == "" {
				path = ws.BasePath()
			}
			cmd := fmt.Sprintf("grep -rn '%s' '%s'", shellQuote(pattern), shellQuote(path))
			res, err := ws.Execute(ctx, cmd)
			if err != nil {
				return nil, err
			}
			return res.Stdout, nil
		},
	)
}

// shellQuote escapes a string for safe single-quote wrapping in a shell
// command (replaces ' with '\''). Within a sandbox the model already has
// arbitrary shell via Bash, so this is for correctness, not security.
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
