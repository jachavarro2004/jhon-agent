package discover

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"slices"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// allWorkspaces are the root test scripts that run the test script of every
// workspace member. Only these spellings are recognised; a script that merely
// looks broad (a Makefile target, a shell loop) is `unknown`, because reading
// intent into it is exactly the guess coverage exists to avoid.
var allWorkspaces = []*lazyre.Regexp{
	lazyre.New(`\b(?:npm|yarn)\b` + js.Dot + `*(?:--workspaces\b|` + js.Space + `-ws\b)`),
	lazyre.New(`\bpnpm\b` + js.Dot + `*(?:` + js.Space + `-r\b|--recursive\b)`),
	lazyre.New(`\bturbo` + js.Space + `+run` + js.Space + `+test\b`),
	lazyre.New(`\bnx` + js.Space + `+run-many\b` + js.Dot + `*\btest\b`),
}

var (
	jsSources        = []string{"package.json workspaces", "pnpm-workspace.yaml packages"}
	workspaceSection = lazyre.New(`(?m)` + js.LineStart + `\[workspace\]`)
	packageSection   = lazyre.New(`(?m)` + js.LineStart + `\[package\]`)
)

// coverage is whether one command covers every discovered project, decided
// only for the recognised cases, in this order:
//
//   - zero projects: `no`, since there is nothing a command could cover;
//   - exactly one project with a test command: `yes`, that command (a single
//     go.mod lands here, and `go test ./...` covers every package of it);
//   - a Cargo virtual workspace root (`[workspace]` without `[package]`) whose
//     members are all the other projects: `yes`, `cargo test` at the root;
//   - a root package.json `scripts.test` in allWorkspaces, when every other
//     project is a declared JS workspace member: `yes`, the root's command;
//   - no project with any test command: `no`. Discovery reads test commands
//     from explicit files, and a workspace command would itself be the root's;
//   - anything else: `unknown`, with the reason.
func coverage(root string, projects []*Project, members map[string]map[string]bool) Coverage {
	v := vocab()
	if len(projects) == 0 {
		return Coverage{v.notCovered, nil, "no project was discovered"}
	}
	withCommand := 0
	for _, p := range projects {
		if p.TestCommand != nil {
			withCommand++
		}
	}
	if len(projects) == 1 && withCommand == 1 {
		p := projects[0]
		where := p.Path + "/"
		if p.Path == "." {
			where = "the workspace root"
		}
		return Coverage{v.covered, p.TestCommand, fmt.Sprintf("the only discovered project runs %s (%s) from %s", *p.TestCommand, *p.TestCommandSource, where)}
	}
	var others []string
	var rootProject *Project
	for _, p := range projects {
		if p.Path == "." {
			rootProject = p
		} else {
			others = append(others, p.Path)
		}
	}
	declaredBy := func(sources []string) bool {
		for _, rel := range others {
			if !slices.ContainsFunc(sources, func(s string) bool { return members[s][rel] }) {
				return false
			}
		}
		return true
	}

	cargo, _ := read(filepath.Join(root, "Cargo.toml"))
	if workspaceSection.MatchString(cargo) && !packageSection.MatchString(cargo) && len(others) > 0 && declaredBy([]string{cargoSource}) {
		return Coverage{v.covered, str("cargo test"), "Cargo.toml is a virtual workspace root; cargo test there runs every member"}
	}

	src, ok := read(filepath.Join(root, "package.json"))
	if !ok {
		src = "{}"
	}
	pkg, _ := parseJSON(src)
	script, isString := field(field(pkg, "scripts"), "test").(string)
	if rootProject != nil && rootProject.TestCommand != nil && isString &&
		slices.ContainsFunc(allWorkspaces, func(re *lazyre.Regexp) bool { return re.MatchString(script) }) &&
		len(others) > 0 && declaredBy(jsSources) {
		return Coverage{v.covered, rootProject.TestCommand, fmt.Sprintf("the root scripts.test (%s) runs every declared workspace member", script)}
	}

	// `no` asserts there is nothing to run. A nil command that carries a reason
	// means tests are configured but no single command is certain, so it proves
	// nothing either way: coverage stays unknown rather than claim what cannot
	// be shown.
	undetermined := slices.ContainsFunc(projects, func(p *Project) bool { return p.TestCommand == nil && p.TestCommandSource != nil })
	if withCommand == 0 && !undetermined {
		return Coverage{v.notCovered, nil, fmt.Sprintf("none of the %d discovered projects has a test command in any file", len(projects))}
	}
	return Coverage{v.coverageUnknown, nil, fmt.Sprintf("%d projects and no recognised workspace-level command covers them; informational only, since each project runs its own resolved command", len(projects))}
}
