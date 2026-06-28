// Package workspace builds a sandboxed tool toolkit that routes Bash/Read/
// Write/Edit/Glob/Grep through an agentscope workspace.Workspace (e.g. a
// Docker container or E2B cloud sandbox). Used when lathe runs with
// --sandbox; without it, lathe uses the host agentscope builtins.
package workspace

import (
	"context"
	"encoding/json"
	"fmt"

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
