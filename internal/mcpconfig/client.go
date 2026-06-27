package mcpconfig

import (
	"context"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/mcp"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// MCPGroup is the set of tools discovered from one MCP server.
type MCPGroup struct {
	Name  string
	Tools []tool.Tool
}

// BuildClients builds an mcp.Client per server config, discovers tools, and
// returns the live clients (for Close on shutdown), the tool groups (for
// AddGroup), and warnings for servers that failed to start or list tools.
// A failing server is skipped (and closed if partially built); others
// continue.
func BuildClients(ctx context.Context, cfgs []ServerConfig) (clients []mcp.Client, groups []MCPGroup, warnings []string) {
	for _, c := range cfgs {
		client, w, ok := buildOne(ctx, c)
		warnings = append(warnings, w...)
		if !ok {
			continue
		}
		schemas, err := client.ListTools(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("server %q: list tools: %v", c.Name, err))
			_ = client.Close()
			continue
		}
		var tools []tool.Tool
		for _, s := range schemas {
			tools = append(tools, mcp.NewMCPTool(client, s))
		}
		clients = append(clients, client)
		groups = append(groups, MCPGroup{Name: c.Name, Tools: tools})
	}
	return clients, groups, warnings
}

// buildOne constructs one mcp.Client from a ServerConfig.
func buildOne(ctx context.Context, c ServerConfig) (mcp.Client, []string, bool) {
	switch c.Type {
	case "http":
		cl, err := mcp.NewHttpClient(ctx, &mcp.HttpConfig{URL: c.URL, Headers: c.Headers})
		if err != nil {
			return nil, []string{fmt.Sprintf("server %q: http connect: %v", c.Name, err)}, false
		}
		return cl, nil, true
	default: // stdio
		cl, err := mcp.NewStdioClient(ctx, &mcp.StdioConfig{Command: c.Command, Args: c.Args, Env: c.Env, WorkDir: c.WorkDir})
		if err != nil {
			return nil, []string{fmt.Sprintf("server %q: stdio start: %v", c.Name, err)}, false
		}
		return cl, nil, true
	}
}

// Load is the composition entry point: parse .mcp.json then build clients.
func Load(ctx context.Context, cwd string) (clients []mcp.Client, groups []MCPGroup, warnings []string) {
	cfgs, w := LoadConfig(cwd)
	warnings = append(warnings, w...)
	clients, groups, w2 := BuildClients(ctx, cfgs)
	warnings = append(warnings, w2...)
	return clients, groups, warnings
}
