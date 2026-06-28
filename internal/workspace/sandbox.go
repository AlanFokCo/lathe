package workspace

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/workspace"
)

// NewWorkspace builds a sandboxed workspace of the given kind.
//   - "" / "none": returns nil (no sandbox — caller uses host builtins).
//   - "docker": a Docker container mounting cwd at /workspace.
//   - "e2b": an E2B cloud sandbox (requires E2B_API_KEY).
//
// The returned closer stops/removes the workspace (nil for "none"). A
// sandbox setup failure returns an error (lathe does NOT silently fall back
// to host execution — if the user asked for a sandbox, fail loudly).
func NewWorkspace(ctx context.Context, kind, cwd string) (workspace.Workspace, func() error, error) {
	switch kind {
	case "", "none":
		return nil, nil, nil
	case "docker":
		dws, err := workspace.NewDockerWorkspace(ctx, &workspace.DockerWorkspaceConfig{
			Image:          "ubuntu:latest",
			WorkDir:        "/workspace",
			Mounts:         []string{cwd + ":/workspace"},
			CommandTimeout: 30 * time.Second,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("docker: %w", err)
		}
		return dws, func() error { return dws.Close(context.Background()) }, nil
	case "e2b":
		apiKey := os.Getenv("E2B_API_KEY")
		if apiKey == "" {
			return nil, nil, fmt.Errorf("e2b: E2B_API_KEY is required")
		}
		ews, err := workspace.NewE2BWorkspace(ctx, workspace.E2BConfig{APIKey: apiKey})
		if err != nil {
			return nil, nil, fmt.Errorf("e2b: %w", err)
		}
		return ews, func() error { return ews.Close(context.Background()) }, nil
	default:
		return nil, nil, fmt.Errorf("workspace: unknown sandbox kind %q", kind)
	}
}
