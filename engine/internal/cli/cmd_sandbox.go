package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon sandbox` (T128a, odd/proposals/t128-sandbox.md sections 3-5): the OS
// sandbox a model-chosen command runs in.
//
//	jhon sandbox run [--profile work|delivery] [--project <folder>] -- <program> [args...]
//	jhon sandbox status [--json] [--project <folder>]
//	jhon sandbox profile -- <command>
//
// run runs the program under bubblewrap with the program's stdin, stdout,
// stderr, exit code and signals: on a terminal it replaces this process
// with bubblewrap; for a runtime, whose output is a pipe, it runs
// bubblewrap as its child (sandboxSupervised), so it can end a failure on a
// read-only path with the stop note and record a jhon.sandbox event (T128g,
// L1303 G3, G4). It starts no other program, git included, and takes
// bubblewrap only from a PATH folder a sandboxed command cannot write
// (L888). It always sandboxes: where the sandbox cannot run it runs
// nothing, says why and the fix on one line, and exits 126. The one
// exception is the runtimes' --profile unsandboxed, which runs `<shell> -c
// <line>` with no sandbox only when the line is a git or gh network line,
// or a docker one (T128h), of trusted programs (sandbox.RunsOutside), and anything else with the work
// profile. sandbox.mode in the settings is for the callers (the shell, the
// hook), which read it through status: the setting, whether the sandbox
// can run here, and why not. profile prints how a command line runs
// (sandbox.ProfileFor, T128g): `unsandboxed` or `work`; the pi shell asks
// it for each line, and Claude Code's wrap calls the same function.
func init() {
	register("sandbox", runSandbox)
	hook.SandboxInput = func(dir, pinned string) sandbox.Input { return sandboxInputAt(dir, pinned, sandbox.Work) }
}

const sandboxUsage = `usage: jhon sandbox run [--profile work|delivery] [--project <folder>] -- <program> [args...]
       jhon sandbox status [--json] [--project <folder>]
`

// sandboxProfileUsage is the usage of `jhon sandbox profile`, the runtimes'
// question rather than the owner's: the usage above leaves it out, as it
// leaves out run's --profile unsandboxed.
const sandboxProfileUsage = "usage: jhon sandbox profile [--project <folder>] -- <command>\n"

func sandboxUsageError(msg string) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon sandbox: %s\n\n%s", msg, sandboxUsage)
	return 2, nil
}

// sandboxRefused is the exit code of a run the sandbox could not start: a
// shell's "cannot execute".
const sandboxRefused = 126

func runSandbox(argv []string) (int, error) {
	if len(argv) == 0 {
		return sandboxUsageError("run or status is needed")
	}
	switch argv[0] {
	case "run":
		return sandboxRun(argv[1:])
	case "profile":
		pinned, rest := "", argv[1:]
		if len(rest) == 4 && rest[0] == "--project" {
			var ok bool
			if pinned, ok = sandbox.ProjectFolder(rest[1]); !ok {
				fmt.Fprintf(os.Stderr, "jhon sandbox: %s%s\n\n%s", sandboxProjectRefused, rest[1], sandboxProfileUsage)
				return 2, nil
			}
			rest = rest[2:]
		}
		if len(rest) != 2 || rest[0] != "--" {
			fmt.Fprintf(os.Stderr, "jhon sandbox: profile takes one command line after --\n\n%s", sandboxProfileUsage)
			return 2, nil
		}
		in := sandboxInput(pinned, sandbox.Work)
		fmt.Println(sandbox.ProfileAt(rest[1], in.Cwd, in.Roots, in.Stat))
		return 0, nil
	case "status":
		asJSON, pinned := false, ""
		for i := 1; i < len(argv); i++ {
			if argv[i] == "--json" {
				asJSON = true
				continue
			}
			value, ok := strings.CutPrefix(argv[i], "--project=")
			if !ok {
				if argv[i] != "--project" || i+1 >= len(argv) {
					return sandboxUsageError("unknown status option: " + argv[i])
				}
				i++
				value = argv[i]
			}
			if pinned, ok = sandbox.ProjectFolder(value); !ok {
				return sandboxUsageError(sandboxProjectRefused + value)
			}
		}
		return sandboxStatus(asJSON, pinned)
	}
	return sandboxUsageError("unknown subcommand: " + argv[0])
}

// sandboxProjectRefused is the usage error of a --project that names no
// folder a project may be; the value follows it.
const sandboxProjectRefused = "--project takes an absolute folder that exists and is not /, the home folder or one above it: "

// sandboxRun is `jhon sandbox run`. --project pins the project: the
// runtimes pass the root they resolved when the session started, so a
// command that removed the project's `.git` entry does not widen the next
// start to an enclosing repository; without it the project is found by
// walking up from the working directory.
func sandboxRun(argv []string) (int, error) {
	profile, pinned := sandbox.Work, ""
	i := 0
	for ; i < len(argv) && argv[i] != "--"; i++ {
		name, value, ok := strings.Cut(argv[i], "=")
		if !ok {
			if i+1 >= len(argv) {
				return sandboxUsageError("run takes a program after --")
			}
			i++
			value = argv[i]
		}
		switch name {
		case "--profile":
			if !slices.Contains(sandbox.Profiles, value) && value != sandbox.Unsandboxed {
				return sandboxUsageError("unknown profile: " + value + " (accepted: " + strings.Join(sandbox.Profiles, ", ") + ")")
			}
			profile = value
		case "--project":
			if pinned, ok = sandbox.ProjectFolder(value); !ok {
				return sandboxUsageError(sandboxProjectRefused + value)
			}
		default:
			return sandboxUsageError("run takes a program after --")
		}
	}
	if i+1 >= len(argv) {
		return sandboxUsageError("run takes a program after --")
	}
	program := argv[i+1:]
	if profile == sandbox.Unsandboxed {
		// git runs with the repository's hooks, ssh command, helpers and
		// transports overridden (sandbox.UnsandboxedEnv, L1306).
		if sandbox.RunsOutside(sandbox.ThisHost(), sandboxInput(pinned, sandbox.Work), program) {
			return sandboxProgram(program[0], program, sandbox.UnsandboxedEnv(os.Environ(), ownerCredentialHelpers()), sandbox.Unsandboxed)
		}
		profile = sandbox.Work
	}
	in := sandboxInput(pinned, profile)
	bwrap, p := sandbox.Check(sandbox.ThisHost(), in)
	if p != nil {
		fmt.Fprintf(os.Stderr, "jhon sandbox: %s, so nothing was run; fix: %s\n", p.Reason, p.Fix)
		return sandboxRefused, nil
	}
	env := os.Environ()
	if profile == sandbox.Work {
		env = append(env, sandbox.UVToolEnv(in)...)
	}
	return sandboxProgram(bwrap, append(append([]string{bwrap}, sandbox.Args(in)...), argv[i:]...), env, profile)
}

// sandboxInput is the sandbox of a program started here, in the pinned
// project or with "" the one this folder is in (sandboxInputAt).
func sandboxInput(pinned, profile string) sandbox.Input {
	return sandboxInputAt(cwd(), pinned, profile)
}

// sandboxInputAt is the sandbox of a program started in dir, in the pinned
// project or with "" the one dir is in: the project's areas, and what the
// settings and the integrations add. Jhon's own files are named by the code
// that owns each: the settings file (and its folder, unless JHON_SETTINGS
// names a file elsewhere), the state folder that holds the grants and
// the modes, and Jhon's source checkout (sourceCheckout). The guard hook
// probes with the same one (hook.SandboxInput): telemetry is an
// integration the hooks never import (T55).
func sandboxInputAt(dir, pinned, profile string) sandbox.Input {
	in := sandbox.Current(dir, pinned, profile)
	in.AllowWrite = settings.Paths("sandbox.allow_write")
	in.Protected = []string{settings.Path(), filepath.Dir(grant.Dir())}
	if os.Getenv("JHON_SETTINGS") == "" {
		in.Protected = append(in.Protected, filepath.Dir(settings.Path()))
	}
	if source := sourceCheckout(in); source != "" {
		in.Protected = append(in.Protected, source)
	}
	in.Telemetry = telemetry.Dir()
	return in
}

// sourceCheckout is Jhon's source checkout when the sandbox of in keeps it
// read-only, else "": the roots' Source (release.Locate: the checkout run
// directly, JHON_SOURCE, or the one `jhon sync` recorded), unless it is the
// project itself. A linked worktree of it is another project: its main
// worktree is writable there, and the checkout would be with it (T128f
// slice 7, L1367 B1). Its git folder stays writable inside it: the
// worktree commits through it.
func sourceCheckout(in sandbox.Input) string {
	source, _, ok := in.Stat(release.Locate().Source)
	if !ok {
		return ""
	}
	if len(in.Roots) > 0 {
		if top, _, ok := in.Stat(in.Roots[0]); ok && top == source {
			return ""
		}
	}
	return source
}

// sandboxProject is the project `jhon sandbox run` takes from the working
// directory: the folder that holds `.git` (project.ReadRepo), or "" in no
// repository. It is what `jhon sandbox status` names and what `jhon start`
// pins for the session (sandbox.ProjectEnv).
func sandboxProject() string { return project.ReadRepo(cwd()).Top }

// sandboxState is `jhon sandbox status --json`: sandbox.mode, whether the
// sandbox can run here, the reason and the fix when it cannot, and the
// project a run from here gets: the pinned one, else sandboxProject.
type sandboxState struct {
	Mode string `json:"mode"`
	// ModeSource is where the mode comes from: default (on on Linux where
	// the sandbox can run, off elsewhere, D3) or settings (sandbox.Mode,
	// T128f).
	ModeSource string `json:"mode_source"`
	// ModeReason is why the default is off on Linux: the probe's reason
	// and fix, on one line (T128f slice 8 L1375, 8b L1379); "" otherwise.
	ModeReason string `json:"mode_reason"`
	CanRun     bool   `json:"can_run"`
	Reason     string `json:"reason"`
	Fix        string `json:"fix"`
	Project    string `json:"project"`
	// BashRules is set with the mode on only: with it off the rules apply.
	BashRules *claudeBashRules `json:"claude_bash_rules,omitempty"`
}

// claudeBashRules are the Bash deny and ask rules in Claude Code's settings
// (L899 b). With sandbox.mode on Claude Code matches them against the
// wrapped command, so they stop applying, and the hook cannot honour them:
// the status and the doctor say how many there are, never what they say.
// Unreadable are the settings files that exist and could not be read.
type claudeBashRules struct {
	Deny       int      `json:"deny"`
	Ask        int      `json:"ask"`
	Unreadable []string `json:"unreadable"`
}

// claudeSettingsFiles are the settings files Claude Code documents for a
// session in the project top: the user's (~/.claude/settings.json, or under
// CLAUDE_CONFIG_DIR), the project's and the project's local one.
func claudeSettingsFiles(top string) []string {
	user := os.Getenv("CLAUDE_CONFIG_DIR")
	if user == "" {
		user = filepath.Join(project.Home(), ".claude")
	}
	return []string{filepath.Join(user, "settings.json"), filepath.Join(top, ".claude", "settings.json"), filepath.Join(top, ".claude", "settings.local.json")}
}

// claudeBashRulesIn counts the rules of files whose tool is Bash (`Bash` or
// `Bash(...)`) under permissions.deny and permissions.ask. A file that is
// not there holds none; one that is there and is not a settings object, or
// whose permissions are not lists, is unreadable.
func claudeBashRulesIn(files []string) *claudeBashRules {
	rules := &claudeBashRules{Unreadable: []string{}}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		var doc struct {
			Permissions struct {
				Deny, Ask []any
			} `json:"permissions"`
		}
		if err != nil || json.Unmarshal(data, &doc) != nil {
			rules.Unreadable = append(rules.Unreadable, file)
			continue
		}
		isBash := func(rule any) bool {
			text, _ := rule.(string)
			return text == "Bash" || strings.HasPrefix(text, "Bash(")
		}
		for _, rule := range doc.Permissions.Deny {
			if isBash(rule) {
				rules.Deny++
			}
		}
		for _, rule := range doc.Permissions.Ask {
			if isBash(rule) {
				rules.Ask++
			}
		}
	}
	return rules
}

// warning is what the status and the doctor say of the rules, or "" when
// there is nothing to say: no rule, and no file left unread.
func (r *claudeBashRules) warning() string {
	count := func(n int, kind string) string {
		if n == 1 {
			return "1 Bash " + kind + " rule"
		}
		return strconv.Itoa(n) + " Bash " + kind + " rules"
	}
	switch {
	case r == nil || r.Deny+r.Ask == 0 && len(r.Unreadable) == 0:
		return ""
	case r.Deny+r.Ask == 0:
		return "Claude Code's settings could not be read for Bash deny and ask rules, which do not apply to sandboxed commands (unreadable: " + strings.Join(r.Unreadable, ", ") + ")"
	}
	said := "Claude Code's settings hold " + count(r.Deny, "deny") + " and " + count(r.Ask, "ask") + ", which do not apply to sandboxed commands (Claude Code matches them against the wrapped command)"
	if len(r.Unreadable) > 0 {
		said += "; unreadable: " + strings.Join(r.Unreadable, ", ")
	}
	return said
}

// line is the state on one line, as status and doctor print it.
// The default's off on Linux says so, and that the fix turns the sandbox on.
func (s sandboxState) line() string {
	if s.CanRun {
		return "sandbox " + s.Mode + "; it can run here"
	}
	if s.ModeReason != "" {
		return "sandbox " + s.Mode + " by default; it cannot run here: " + s.Reason + "; fix: " + s.Fix + " to turn the sandbox on"
	}
	return "sandbox " + s.Mode + "; it cannot run here: " + s.Reason + "; fix: " + s.Fix
}

// sandboxOffUnguarded is what doctor says of the owner's sandbox.mode off
// on Linux.
const sandboxOffUnguarded = "with it off the model's shell commands run unguarded outside the project: a write there, Jhon's installed files and source included, is not stopped; fix: jhon settings set sandbox.mode on"

// sandboxHost is the machine the status reads the mode for and probes; a
// test gives another system's.
var sandboxHost = sandbox.ThisHost

// sandboxNow probes the sandbox of a work command started here, in the
// pinned project or with "" the one this folder is in: one empty sandbox
// started for real, with the bubblewrap and the options such a command
// would get. The mode is sandbox.Mode's, with its source and, for the
// default's off on Linux, the probe's reason and fix; the mode and the
// probe read the same sandbox, built once, and bubblewrap starts once.
func sandboxNow(pinned string) sandboxState {
	s := sandboxState{CanRun: true, Project: pinned}
	in := sandboxInput(pinned, sandbox.Work)
	e := sandbox.Mode(sandboxHost(), func() sandbox.Input { return in })
	s.Mode, s.ModeSource = e.Mode, e.Source
	if e.Mode == "off" && e.Problem != nil {
		s.ModeReason = e.Problem.Line()
	}
	if pinned == "" {
		s.Project = sandboxProject()
	}
	// One empty sandbox started: the mode's probe when it probed.
	p := e.Problem
	if !e.Probed {
		p = sandbox.Probe(sandboxHost(), in)
	}
	if p != nil {
		s.CanRun, s.Reason, s.Fix = false, p.Reason, p.Fix
	}
	if s.Mode == "on" {
		top := s.Project
		if top == "" {
			top = cwd()
		}
		s.BashRules = claudeBashRulesIn(claudeSettingsFiles(top))
	}
	return s
}

func sandboxStatus(asJSON bool, pinned string) (int, error) {
	s := sandboxNow(pinned)
	if asJSON {
		return 0, encodeJSON(s)
	}
	where := "; no project here (no git repository)"
	if s.Project != "" {
		where = "; project " + s.Project
	}
	if w := s.BashRules.warning(); w != "" {
		where += "; warning: " + w
	}
	fmt.Println(s.line() + where)
	return 0, nil
}

// sandbox is doctor's sandbox section: the mode and whether the sandbox
// can run. Where it cannot, with the mode set on every command of the
// model is refused: a problem, with the fix (T128f, L1352 F2); with the mode
// off, the default off Linux (D3) or on Linux where it cannot run (slices 8
// and 8b, L1375, L1379), a warning that the commands run unsandboxed, never
// a problem. The owner's off on Linux,
// where it can run, is a warning too: the guards leave the shell's writes
// outside the project to the sandbox, so they then run unguarded (T128f
// slice 7, L1367 gap 2). With the mode on, a second warning counts the Bash
// rules of Claude Code that no longer apply.
func (d doctor) sandbox() {
	d.o.Head("sandbox")
	s := sandboxNow("")
	switch {
	case s.CanRun && s.Mode == "off" && s.ModeSource == sandbox.ModeSettings && sandboxHost().OS == "linux":
		d.o.Warn(s.line() + "; " + sandboxOffUnguarded)
	case s.CanRun:
		d.o.OK(s.line())
	case s.Mode == "on":
		d.o.Bad(s.line() + "; until then every command of the model is refused")
	default:
		d.o.Warn(s.line() + "; the model's commands run unsandboxed")
	}
	if w := s.BashRules.warning(); w != "" {
		d.o.Warn(w)
	}
}
