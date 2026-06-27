// Package mcpconfig parses lathe's .mcp.json (project + user) into server
// configs and builds agentscope mcp.Clients + tool groups. MCP servers
// declared here become ordinary tools in the engine's toolkit.
package mcpconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/mcp"
)

// ServerConfig is one MCP server entry from .mcp.json.
// Type is "http" for HTTP servers, "stdio" for subprocess servers.
type ServerConfig struct {
	Name    string
	Type    string // "stdio" | "http"
	Command string
	Args    []string
	Env     []string // "KEY=VALUE" entries
	WorkDir string
	URL     string
	Headers map[string]string
}

// LoadConfig reads <cwd>/.mcp.json and ~/.lathe/mcp.json, merges them (a
// project server overrides a user server of the same Name), validates each
// server name, and drops unsupported types (e.g. "sse") and entries missing
// required fields. Returns the merged configs and warnings for skipped
// entries. Missing files are silent; parse errors become warnings.
func LoadConfig(cwd string) ([]ServerConfig, []string) {
	var warnings []string
	var cfgs []ServerConfig
	byName := map[string]int{} // name → index into cfgs

	add := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return // missing file is silent
		}
		var doc struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: parse: %v", path, err))
			return
		}
		for name, raw := range doc.MCPServers {
			if err := mcp.ValidateMCPName(name); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			sc, w := parseServer(name, raw)
			warnings = append(warnings, w...)
			if sc == nil {
				continue
			}
			if idx, ok := byName[name]; ok {
				cfgs[idx] = *sc // project (loaded later) overrides user
			} else {
				byName[name] = len(cfgs)
				cfgs = append(cfgs, *sc)
			}
		}
	}

	// user first, then project (project overrides user on same name)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".lathe", "mcp.json"))
	}
	if cwd != "" {
		add(filepath.Join(cwd, ".mcp.json"))
	}
	return cfgs, warnings
}

// parseServer decodes a raw server map into a ServerConfig. Returns nil + a
// warning for unsupported types or missing required fields.
func parseServer(name string, raw map[string]any) (*ServerConfig, []string) {
	typ, _ := raw["type"].(string)
	switch typ {
	case "", "stdio":
		cmd, _ := raw["command"].(string)
		if cmd == "" {
			return nil, []string{fmt.Sprintf("server %q: stdio requires \"command\"", name)}
		}
		sc := &ServerConfig{Name: name, Type: "stdio", Command: cmd}
		if a, ok := raw["args"].([]any); ok {
			for _, x := range a {
				if s, ok := x.(string); ok {
					sc.Args = append(sc.Args, s)
				}
			}
		}
		if e, ok := raw["env"].(map[string]any); ok {
			for k, v := range e {
				if s, ok := v.(string); ok {
					sc.Env = append(sc.Env, k+"="+s)
				}
			}
		}
		if wd, ok := raw["cwd"].(string); ok {
			sc.WorkDir = wd
		}
		return sc, nil
	case "http":
		url, _ := raw["url"].(string)
		if url == "" {
			return nil, []string{fmt.Sprintf("server %q: http requires \"url\"", name)}
		}
		sc := &ServerConfig{Name: name, Type: "http", URL: url}
		if h, ok := raw["headers"].(map[string]any); ok {
			sc.Headers = map[string]string{}
			for k, v := range h {
				if s, ok := v.(string); ok {
					sc.Headers[k] = s
				}
			}
		}
		return sc, nil
	case "sse":
		return nil, []string{fmt.Sprintf("server %q: type \"sse\" not yet supported (skipped)", name)}
	default:
		return nil, []string{fmt.Sprintf("server %q: unknown type %q (skipped)", name, typ)}
	}
}
