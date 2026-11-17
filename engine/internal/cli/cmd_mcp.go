package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"

	"github.com/jachavarro2004/jhon-agent/engine/internal/mcp"
)

// `jhon mcp`: the read-only MCP server over stdio
// (internal/mcp). Each tool call runs this binary with the jhon subcommand
// (audit, status), so the server never needs node.
func init() { register("mcp", runMCP) }

func runMCP([]string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 1, err
	}
	run := func(args []string) (string, string, int, bool) {
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(exe, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return stdout.String(), stderr.String(), 0, true
		case errors.As(err, &exit) && exit.Exited():
			return stdout.String(), stderr.String(), exit.ExitCode(), true
		}
		return stdout.String(), stderr.String(), -1, false
	}
	return 0, mcp.Serve(os.Stdin, os.Stdout, run)
}
