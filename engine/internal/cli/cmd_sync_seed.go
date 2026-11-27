package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// The seed of the launch check (T266, the owner's decision in L1768). A
// root `jhon start` never saw asks once when anything runs there, so the
// owner's own projects would each ask at their first start. `jhon sync`,
// from the owner's terminal, records them as they stand once: every root
// under the sandbox.allow_write folders (each repository found there, its
// main checkout and its worktrees, and the folder itself) and every
// project with Jhon state (a grant, a mode, a workspace pointer), each
// root that has no record yet. It shows the roots where something runs,
// with how many files run there and no line of theirs, and asks once;
// with no terminal it skips, in one line. It never seeds inside the
// sandbox, and the guards ask an agent's `jhon sync` at the top level and
// refuse it to a sub-agent, as they do --approve-hooks.

// seedDepth is how deep under an allow_write folder a repository is
// looked for.
const seedDepth = 3

// seedHooks is the seed, before the sync proper.
func seedHooks(o *release.Out) {
	if sandbox.Inside() {
		return
	}
	type root struct {
		path  string
		files []hookFile
	}
	var runs, none []root
	for _, r := range seedRoots() {
		if _, err := os.Stat(hookRecordPath(r)); err == nil {
			continue
		}
		files := hookSignature(r)
		if len(files) == 0 {
			none = append(none, root{r, files})
		} else {
			runs = append(runs, root{r, files})
		}
	}
	if len(runs) == 0 {
		for _, r := range none {
			_ = writeHookRecord(newHookRecord(r.path, r.files))
		}
		return
	}
	if !isTerminal(os.Stdin) {
		fmt.Fprintf(o.E, "%sjhon:%s the hook seed was skipped: no terminal to ask on; %s where something runs will ask at the first `jhon start` there\n",
			o.R, o.Z, plural(len(runs), "folder"))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%sjhon:%s what runs outside the sandbox in %s you never started in, to record as approved:\n",
		o.R, o.Z, plural(len(runs), "folder"))
	for _, r := range runs {
		verb := "run"
		if len(r.files) == 1 {
			verb = "runs"
		}
		fmt.Fprintf(&b, "  %s: %s that %s\n", r.path, plural(len(r.files), "file"), verb)
	}
	if len(none) > 0 {
		fmt.Fprintf(&b, "  and %s where nothing runs\n", plural(len(none), "folder"))
	}
	fmt.Fprintf(&b, "%sjhon:%s `jhon start` asks once in each of them unless you record them now; record them? [y/N] ", o.R, o.Z)
	fmt.Fprint(o.E, b.String())
	if !yes(readAnswer(os.Stdin)) {
		fmt.Fprintf(o.E, "%sjhon:%s nothing was recorded\n", o.R, o.Z)
		return
	}
	for _, r := range append(runs, none...) {
		if err := writeHookRecord(newHookRecord(r.path, masked(r.files))); err != nil {
			fmt.Fprintf(o.E, "%sjhon:%s %s was not recorded: %v\n", o.R, o.Z, r.path, err)
		}
	}
	fmt.Fprintf(o.E, "%sjhon:%s recorded %s\n", o.R, o.Z, plural(len(runs)+len(none), "folder"))
}

// seedRoots are the roots the seed records, each once, sorted: the
// repositories under the allow_write folders and the folders themselves,
// and the projects with Jhon state, each with its main checkout and its
// worktrees; never / or the home.
func seedRoots() []string {
	home := project.Realish(project.Home())
	set := map[string]bool{}
	add := func(dir string) {
		d := project.Realish(dir)
		if d == "/" || d == home {
			return
		}
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			set[d] = true
		}
	}
	addRepo := func(dir string) {
		repo := project.ReadRepo(dir)
		if repo.Top == "" {
			return
		}
		add(repo.Top)
		if m := project.RepoMain(repo); m != "" {
			add(m)
		}
		for _, w := range repo.Worktrees() {
			add(w)
		}
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			addRepo(dir)
			return
		}
		if depth == 0 {
			return
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "node_modules" {
				walk(filepath.Join(dir, e.Name()), depth-1)
			}
		}
	}
	for _, f := range settings.Paths("sandbox.allow_write") {
		add(f)
		addRepo(f)
		walk(project.Realish(f), seedDepth)
	}
	for _, r := range grant.Records() {
		addRepo(r.Root)
	}
	for _, c := range grant.ModeHistory() {
		addRepo(c.Root)
	}
	for _, p := range workspace.Pointers() {
		addRepo(p.Root)
		addRepo(p.Primary)
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
