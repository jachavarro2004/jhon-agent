package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
)

// formatTimeout bounds one formatter run.
const formatTimeout = 20 * time.Second

// FormatEdit is the PostToolUse hook on a writer's edits (docket AY B2): it
// rewrites the edited file with its project's own formatter
// (discover.FormatterFor), so style is never the agent's call. It never
// fails the edit: a file no project formats, a formatter that is missing or
// refuses the file (a syntax error the format check will report), and a
// payload it cannot read all exit 0 silently. When the file changed it tells
// the agent, whose copy of the file is now stale.
func FormatEdit(_ []string, io IO) int {
	p, ok := read(io.In)
	if !ok {
		return 0
	}
	file := p.Obj("tool_input").Str("file_path")
	if file == "" {
		return 0
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(p.Cwd(), file)
	}
	f := discover.FormatterFor(file)
	if f == nil {
		return 0
	}
	before, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.Argv[0], f.Argv[1:]...)
	cmd.Dir = f.Dir
	if cmd.Run() != nil {
		return 0
	}
	if after, err := os.ReadFile(file); err != nil || bytes.Equal(before, after) {
		return 0
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "PostToolUse",
		"additionalContext": fmt.Sprintf("jhon: %s reformatted %s; read it again before the next edit", f.Tool, f.Argv[len(f.Argv)-1]),
	}})
	fmt.Fprintln(io.Out, string(out))
	return 0
}
