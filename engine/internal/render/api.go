// Package render projects the Jhon manifest onto each runtime:
// Claude Code (claude.go) and pi (pi.go), each a
// Renderer.
//
// It generates only what cannot be referenced: agent metadata and the
// settings each runtime reads. All prose (contracts, orchestrator,
// persona, agent bodies) stays in the release and is carried whole. Every
// run validates the manifest and the prose for internal consistency before
// writing anything (validate.go): drift detection compares bytes, and a
// broken cross-reference is byte-identical to a correct one.
//
// The exported API is what the CLI calls; each function is one renderer
// invocation, with its output and exit code:
//
//	Run(root, args, stdout, stderr)        jhon render-engine <args…>
//	Validate(source, stdout, stderr)       --validate on the source
//	Check(source, stdout, stderr)          --check on the source
//	Preflight(release, source, out, err)   --preflight --source <source>, from a release
//	Install(release, source, out, err)     --install --source <source>, from a release
//	Print(root, agent, stdout, stderr)     --print <agent>
//	RuntimeDoctor(root, args, out, err)    jhon doctor --runtime
//	AlwaysOnBundle(root, runtime)          the always-on layer a runtime without imports receives
//
// root is the tree the renderer runs from: the source checkout, or a
// release under ~/.local/share/jhon/releases/<commit> (symlinks resolved).
// $HOME, JHON_PROFILES, and JHON_DESKTOP_COLORS are read from the
// environment.
package render

import (
	"io"
	"path/filepath"
	"strings"
)

// Validate is `--validate`: the repository at source alone,
// writing nothing; 0 when it validates, 1 when it does not.
func Validate(source string, stdout, stderr io.Writer) int {
	return Run(source, []string{"--validate"}, stdout, stderr)
}

// Check is `--check`: validate, then report drift between the
// repository and what is installed in $HOME; 1 on drift or a problem.
func Check(source string, stdout, stderr io.Writer) int {
	return Run(source, []string{"--check"}, stdout, stderr)
}

// Print is `--print <agent>`: the agent's rendered Claude Code
// frontmatter; writes nothing.
func Print(root, agent string, stdout, stderr io.Writer) int {
	return Run(root, []string{"--print", agent}, stdout, stderr)
}

// AlwaysOnBundle is the always-on layer as a runtime without imports
// receives it: every file concatenated, then that runtime's guarantees.
func AlwaysOnBundle(root, runtime string) (string, error) {
	e, err := newEngine(resolveRoot(root), args{}, io.Discard, io.Discard)
	if err != nil {
		return "", err
	}
	return e.alwaysOnBundle(runtime), nil
}

// resolveRoot is root as node knows a script's directory: absolute, with
// its symlinks resolved.
func resolveRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return strings.TrimSuffix(abs, "/")
}
