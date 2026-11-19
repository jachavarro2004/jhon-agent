package hook

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// SecretsGuard refuses every agent's Edit, MultiEdit, Write, or NotebookEdit
// of a file that holds credentials (secrets-guard), and of any file in a
// home credentials directory: the owner handles them. A template of one
// (`.env.example`) passes like any file. A read or search (Read, Grep,
// Glob) is not judged: the always-on prose says never to read one (T151,
// L1366, L1369). A shell command that writes or deletes one is
// safety-guard's.
func SecretsGuard(args []string, io IO) int {
	scoped, name, _ := owner(args)
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "secrets-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if scoped && name == "" {
		return block("--agent was given without an agent name; the render is broken")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	tool, input := p.Str("tool_name"), p.Obj("tool_input")
	n := ""
	switch tool {
	case "Edit", "Write", "MultiEdit":
		n = input.Str("file_path")
	case "NotebookEdit":
		n = input.Str("notebook_path")
	}
	if n == "" {
		return 0
	}
	if guard.CredentialFile(n) {
		return block(tool + " of " + guard.CredentialsWhy(n) + ".")
	}
	cwd := p.Str("cwd")
	if !filepath.IsAbs(cwd) {
		cwd, _ = os.Getwd()
	}
	// An edit tool's write in a home credentials directory is refused under any name (T94, L638).
	if guard.HomeCredentialPath(n, cwd) {
		return block(tool + " of " + n + ", in a home credentials directory.")
	}
	return 0
}
