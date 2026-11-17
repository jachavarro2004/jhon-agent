package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// validateRuntime judges what was actually projected onto the runtime,
// where a stale artifact survives a clean repository. Every check is
// skipped when its artifact is absent, so a machine that has never
// rendered still validates.
func (e *engine) validateRuntime() []problem {
	var problems []problem
	add := func(file, rule, msg string) { problems = append(problems, problem{e.tilde(file), 0, rule, msg}) }

	// Marker integrity is checked in every mode: a render cannot repair
	// it and must write nothing. A leftover block is installed state,
	// which --check reports as drift. Claude Code turned off in
	// settings.runtimes is not judged: its files are left as they are.
	claude := e.on("claude-code")
	if claude && exists(e.claudeMd()) {
		if _, _, _, why := claudeMdBlock(mustReadText(e.claudeMd())); why != "" {
			add(e.claudeMd(), "claude-md-markers", why)
		}
	}

	// Anything the docs tell a human or an agent to run must exist and be
	// executable. The pre-commit hook is the README's, the checkout's
	// (inRelease).
	for _, rel := range []string{"tools/git-hooks/pre-commit"} {
		if e.inRelease() {
			break
		}
		p := filepath.Join(e.root, rel)
		if fi, err := os.Stat(p); err != nil {
			add(p, "executable", "is referenced as a command but does not exist")
		} else if fi.Mode().Perm()&0o111 == 0 {
			add(p, "executable", "exists but is not executable (chmod +x)")
		}
	}

	// A dangling symlink fails silently: the file stops being discovered.
	// An install, and sync's preflight, judge the runtime as the install
	// leaves it: a link through `current` resolves in this release, and an
	// obsolete command link is pruned, not dangling.
	prospective := e.args.install || e.args.preflight
	var obsolete []string
	if prospective {
		obsolete = e.obsoleteCommands()
	}
	resolves := func(p string) bool {
		target := resolveLink(p)
		if cur := e.current() + "/"; prospective && strings.HasPrefix(target, cur) {
			return exists(filepath.Join(e.root, target[len(cur):]))
		}
		return exists(p)
	}
	for _, dir := range []string{e.path(".claude/commands"), e.path(".claude/agents")} {
		if !claude {
			break
		}
		for _, f := range readDir(dir) {
			p := filepath.Join(dir, f.Name())
			if slices.Contains(obsolete, p) {
				continue
			}
			if isLink(p) && !resolves(p) {
				t, _ := os.Readlink(p)
				add(p, "dangling-symlink", "points at "+t+", which does not exist")
			}
		}
	}
	return problems
}
