package mcpconfig

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// startMockMCPServer starts a minimal JSON-RPC MCP HTTP server exposing one
// tool named toolName, returns its base URL. Closed via t.Cleanup.
func startMockMCPServer(t *testing.T, toolName string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "mock", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": toolName, "description": "mock", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + l.Addr().String()
}

func TestBuildClientsHTTPDiscoversTools(t *testing.T) {
	url := startMockMCPServer(t, "mocktool")
	cfgs := []ServerConfig{{Name: "m", Type: "http", URL: url}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clients, groups, w := BuildClients(ctx, cfgs)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(clients) != 1 {
		t.Fatalf("clients: %d", len(clients))
	}
	if len(groups) != 1 || groups[0].Name != "m" {
		t.Fatalf("groups: %+v", groups)
	}
	if len(groups[0].Tools) != 1 || groups[0].Tools[0].Name() != "mocktool" {
		t.Fatalf("tools: %+v", groups[0].Tools)
	}
	if err := clients[0].Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBuildClientsBadServerSkipped(t *testing.T) {
	cfgs := []ServerConfig{{Name: "bad", Type: "http", URL: "http://127.0.0.1:1/mcp"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clients, groups, w := BuildClients(ctx, cfgs)
	if len(clients) != 0 || len(groups) != 0 {
		t.Fatalf("bad server should be skipped: clients=%d groups=%+v", len(clients), groups)
	}
	if len(w) == 0 {
		t.Fatal("want warning for bad server")
	}
}

func TestLoadEndToEnd(t *testing.T) {
	url := startMockMCPServer(t, "loadtool")
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	writeMCPJSON(t, filepath.Join(cwd, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"m": map[string]any{"type": "http", "url": url}}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clients, groups, w := Load(ctx, cwd)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(clients) != 1 || len(groups) != 1 || groups[0].Tools[0].Name() != "loadtool" {
		t.Fatalf("load: clients=%d groups=%+v", len(clients), groups)
	}
	_ = clients[0].Close()
}
