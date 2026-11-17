package cli

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// `jhon init [dir] [--json]`: read-only project
// discovery. It reports what discover.Discover finds and writes nothing;
// the LDD init phase is the one writer of openspec/config.yaml.
func init() { register("init", runInit) }

func runInit(args []string) (code int, err error) {
	// A broken vocabulary fails the command.
	defer func() {
		if r := recover(); r != nil {
			code, err = 1, fmt.Errorf("%v", r)
		}
	}()
	json := slices.Contains(args, "--json")
	rest := slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--json" })
	if len(rest) > 1 || slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "-") }) {
		fmt.Fprintln(os.Stderr, "usage: jhon init [dir] [--json]   (read-only project discovery)")
		return 2, nil
	}
	var root string
	if len(rest) == 1 && rest[0] != "" {
		root = project.Resolve(cwd(), rest[0])
	} else {
		root = defaultRoot()
	}
	if st, err := os.Lstat(root); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "jhon init: %s is not a directory\n", root)
		return 2, nil
	}
	d := discover.Discover(root)
	if !json {
		fmt.Println(human(d))
		return 0, nil
	}
	out, err := js.Stringify(d)
	if err != nil {
		return 1, err
	}
	fmt.Println(out)
	return 0, nil
}

// defaultRoot is the workspace root when none is given: the enclosing git
// repository, else the current directory. git runs with the caller's
// environment.
func defaultRoot() string {
	c := exec.Command("git", "rev-parse", "--show-toplevel")
	c.Env = project.GitReadEnv()
	out, err := c.Output()
	if top := strings.TrimSpace(string(out)); err == nil && top != "" {
		return top
	}
	return cwd()
}

func human(d *discover.Discovery) string {
	var out []string
	add := func(lines ...string) { out = append(out, lines...) }
	add("workspace   " + d.Root)
	if d.Membership == "declared" {
		add(fmt.Sprintf("membership  declared (%s)", strings.Join(d.DeclaredBy, ", ")))
	} else {
		add(fmt.Sprintf("membership  scanned (the root and at most %d levels below it)", discover.MaxDepth))
	}
	add("", fmt.Sprintf("projects    %d", len(d.Projects)))
	w, s := 4, 5
	for _, p := range d.Projects {
		w = max(w, js.Len(p.Path))
		s = max(s, js.Len(strings.Join(p.Stacks, "+")))
	}
	for _, p := range d.Projects {
		cmd := "— no test command in any file"
		if p.TestCommand != nil {
			cmd = fmt.Sprintf("%s  (%s)", *p.TestCommand, *p.TestCommandSource)
		} else if p.TestCommandSource != nil {
			cmd = "— no test command: " + *p.TestCommandSource
		}
		add(fmt.Sprintf("  %s  %s  %s", js.PadEnd(p.Path, w), js.PadEnd(strings.Join(p.Stacks, "+"), s), cmd))
		add(fmt.Sprintf("  %s  testability %s  (%s)", js.PadEnd("", w), p.Testability, p.TestabilityReason))
	}
	command := ""
	if d.Coverage.Command != nil {
		command = fmt.Sprintf("  (%s)", *d.Coverage.Command)
	}
	add("", fmt.Sprintf("coverage    %s%s  %s", d.Coverage.Covered, command, d.Coverage.Reason))
	exists := "absent"
	if d.OpenspecConfig.Exists {
		exists = "exists"
	}
	add("", d.OpenspecConfig.Path+"  "+exists)
	add("", fmt.Sprintf("vcs         %s  (%s)", d.Vcs.State, d.Vcs.Reason))
	if len(d.Notes) > 0 {
		add("", "notes")
		for _, n := range d.Notes {
			add("  - " + n)
		}
	}
	add("", "read-only: nothing was written. /jhon-ldd init writes openspec/config.yaml.")
	return strings.Join(out, "\n")
}
