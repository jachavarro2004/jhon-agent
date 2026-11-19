package hook

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// SecretsGuard refuses every agent's Read, Grep, Glob, Edit, MultiEdit, Write,
// or NotebookEdit of a file that holds credentials (secrets-guard), and of
// any file in a home credentials directory: the owner handles them. A
// template of one (`.env.example`) passes like any file; a shell command
// that writes or deletes one is safety-guard's; one that reads it is not
// judged (L1366).
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
	var named []string
	glob := ""
	switch tool {
	case "Read", "Edit", "Write", "MultiEdit":
		named = []string{input.Str("file_path")}
	case "NotebookEdit":
		named = []string{input.Str("notebook_path")}
	case "Grep":
		named, glob = []string{input.Str("path")}, input.Str("glob")
	case "Glob":
		// Glob lists names: its pattern is read as Grep's glob is.
		named, glob = []string{input.Str("path")}, input.Str("pattern")
	}
	for _, n := range named {
		if n != "" && (guard.CredentialFile(n) || guard.CredentialGlob(n)) {
			return block(tool + " of " + guard.CredentialsWhy(n) + ".")
		}
	}
	// A glob is read by its name unless the guard proves it reaches no credentials file (guard.ToolGlob).
	cwd := p.Str("cwd")
	if !filepath.IsAbs(cwd) {
		cwd, _ = os.Getwd()
	}
	// A path in a home credentials directory, or a search rooted at or above one, reads it under any name (T87d);
	// an edit tool's write there is refused under any name too (T94, L638).
	switch tool {
	case "Read", "Edit", "MultiEdit", "Write", "NotebookEdit":
		n := input.Str("file_path")
		if tool == "NotebookEdit" {
			n = input.Str("notebook_path")
		}
		if guard.HomeCredentialPath(n, cwd, false) {
			return block(tool + " of " + n + ", in a home credentials directory.")
		}
	case "Grep", "Glob":
		if n := input.Str("path"); guard.HomeCredentialPath(n, cwd, true) {
			if n == "" {
				n = cwd
			}
			return block(tool + " searching " + n + ", which is or reaches into a home credentials directory.")
		}
	}
	if guard.ToolGlob(glob, input.Str("path"), cwd) {
		return block(tool + " of " + guard.CredentialsWhy(glob) + ".")
	}
	return 0
}
