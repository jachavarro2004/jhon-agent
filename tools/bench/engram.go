package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Both Jhon conditions carry engram, as in the owner's real use (L424): pi
// through the bench HOME's ~/.pi/jhon/mcp.json, which the release renders,
// and Claude Code + Jhon through `--mcp-config` with the same server (jhon
// start refuses --plugin-dir: it sets its own, L431). It is the MCP server
// only: the owner's engram plugin hooks talk to an HTTP server on
// 127.0.0.1:7437, the owner's, which the bench never reaches. benchEnv pins
// each run to a store of its own with ENGRAM_DATA_DIR.

// engramMCPConfig is the bench's engram MCP config file in home.
func engramMCPConfig(home string) string {
	return filepath.Join(home, ".local", "share", "jhon-bench", "engram-mcp.json")
}

// engramStore is a run's engram store, under its cache: both conditions use
// engram, and no run recalls what an earlier run saved (L425).
func engramStore(cache string) string { return filepath.Join(cache, "engram") }

// piEngram is the engram server pi's mcp.json in home names, less pi's own
// keys (exposure), so Claude Code gets the same command, args and env.
func piEngram(home string) (map[string]any, error) {
	file := filepath.Join(home, ".pi", "jhon", "mcp.json")
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("no engram server for Claude Code + Jhon: %v (run `bench prepare`)", err)
	}
	var mcp struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &mcp); err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	server, ok := mcp.MCPServers["engram"]
	if !ok {
		return nil, fmt.Errorf("no engram server in %s, so Claude Code + Jhon would have none either (run `bench prepare`)", file)
	}
	delete(server, "exposure")
	return server, nil
}

// writeEngramMCPConfig writes the bench's engram MCP config into home from
// pi's engram server.
func writeEngramMCPConfig(home string) error {
	server, err := piEngram(home)
	if err != nil {
		return err
	}
	file := engramMCPConfig(home)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return writeJSON(file, map[string]any{"mcpServers": map[string]any{"engram": server}})
}
