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
//	jhon sandbox run [--profile work] [--project <folder>] -- <program> [args...]
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
// or a docker one (T128h) or one of the owner's sandbox.deploy_tools, of
// trusted programs (sandbox.RunsOutside), and anything else with the work
// profile; a work run that fails and holds git over SSH or gh ends with a
// note that they run outside the sandbox only on a line of their own
// (sandbox.DeliveryNote, T181), and a work run that holds one of
// sandbox.deploy_tools, failed or not, with the same note for those tools
// (sandbox.DeployNote, T217). The delivery profile is gone (L1503). sandbox.mode in the settings is for the callers (the shell, the
// hook), which read it through status: the setting, whether the sandbox
// can run here, and why not. profile prints how a command line runs
// (sandbox.ProfileFor, T128g): `unsandboxed` or `work`; the pi shell asks
// it for each line, and Claude Code's wrap calls the same function.
func init() {
	register("sandbox", runSandbox)
	hook.SandboxInput = sandboxInputAt
}

const sandboxUsage = `usage: jhon sandbox run [--profile work] [--project <folder>] -- <program> [args...]
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
		in := sandboxInput(pinned)
		fmt.Println(sandbox.ProfileAt(rest[1], in.Cwd, in.Roots, in.Stat, in.DeployTools))
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
	profile, pinned, pinnedAs := sandbox.Work, "", ""
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
			if value != sandbox.Work && value != sandbox.Unsandboxed {
				return sandboxUsageError("unknown profile: " + value + " (accepted: " + sandbox.Work + ")")
			}
			profile = value
		case "--project":
			if pinned, ok = sandbox.ProjectFolder(value); !ok {
				return sandboxUsageError(sandboxProjectRefused + value)
			}
			pinnedAs = value
		default:
			return sandboxUsageError("run takes a program after --")
		}
	}
	if i+1 >= len(argv) {
		return sandboxUsageError("run takes a program after --")
	}
	program := argv[i+1:]
	// The sandbox hides the registry's folder where it exists when the
	// sandbox is read (sandbox.Input.Registry): the first run, which makes
	// it, reads the sandbox again once it is there.
	_, err := os.Stat(runRegistry().Dir)
	registryThere := err == nil
	if profile == sandbox.Unsandboxed {
		// git runs with the repository's hooks, ssh command, helpers and
		// transports overridden (sandbox.UnsandboxedEnv, L1306), only for a
		// repository whose configuration is on the allowlist and whose git
		// folders hold no link (repositoryGitProblem, T249 A2, L39, L40; the
		// line runs in the sandbox otherwise, with a note), and the line
		// with a PATH no sandboxed command may change (sandbox.OutsidePath,
		// T249 A1): what this process runs from here on (git) too.
		// It runs only while no sandboxed command that can write what it
		// reads runs, and none starts until it ends (sandbox.Registry, L34,
		// L37).
		// A line that ends up in the work profile keeps its PATH.
		// Only for the project it was taken from: the working folder or the
		// --project value as written (sandbox.Input.ProjectProblem, L4);
		// anything else runs in the sandbox, which refuses it.
		in := sandboxInput(pinned)
		in.ProjectFrom = "."
		if pinned != "" {
			in.ProjectFrom = pinnedAs
		}
		if in.ProjectProblem() == nil && sandbox.RunsOutside(sandbox.ThisHost(), in, program) {
			leave, refused := enterRegistry(sandbox.KindOutside, sandbox.OutsideAreas(in, program[2]))
			if refused != "" {
				fmt.Fprintln(os.Stderr, refused)
				return sandboxRefused, nil
			}
			path := os.Getenv("PATH")
			os.Setenv("PATH", sandbox.OutsidePath(in))
			gate := mergeGateEntries(pinned)
			why := repositoryGitProblem(in, program[2], gate)
			if why == "" {
				code, err := sandboxProgram(program[0], program, append(sandbox.UnsandboxedEnv(os.Environ(), ownerCredentialHelpers(), gate...), hook.AgentVariable+"=1"), sandbox.Unsandboxed, "", "", nil, nil)
				leave()
				return code, err
			}
			leave()
			os.Setenv("PATH", path)
			fmt.Fprintln(os.Stderr, "jhon sandbox: "+why+"; the line runs in the sandbox instead")
		}
		profile = sandbox.Work
	}
	in := sandboxInput(pinned)
	bwrap, p := runProblem(in)
	// A sandbox.allow_write folder that does not exist yet is made here,
	// outside the sandbox, where that is safe (sandbox.CreateAllowWrite,
	// T239), only once nothing else stops the run: settings a sandboxed
	// command could have written make no folder. The sandbox is then read
	// and checked again with the new folders there.
	var missing []sandbox.MissingFolder
	if p == nil {
		var created []string
		if created, missing = sandbox.CreateAllowWrite(in); len(created) > 0 {
			in = sandboxInput(pinned)
			bwrap, p = runProblem(in)
		}
	}
	if p != nil {
		fmt.Fprintf(os.Stderr, "jhon sandbox: %s, so nothing was run; fix: %s\n", p.Reason, p.Fix)
		return sandboxRefused, nil
	}
	// It runs only while no line that runs outside the sandbox and reads
	// what it can write runs (sandbox.Registry, L34, L37).
	leave, refused := enterRegistry(sandbox.KindSandboxed, sandbox.WritableAreas(in))
	if refused != "" {
		fmt.Fprintln(os.Stderr, refused)
		return sandboxRefused, nil
	}
	if !registryThere {
		in = sandboxInput(pinned)
	}
	// What runs here runs for an agent (T219 S9): git's merge gate judges it.
	env := append(append(os.Environ(), sandbox.UVToolEnv(in)...), hook.AgentVariable+"=1")
	refusal := func(path string) string { return sandbox.Refusal(in, path, missing) }
	// Each bind is opened here, last, with no link followed and a writable
	// one only through the path its checks judged, and bubblewrap mounts
	// the descriptor (sandbox.Open, T248): a folder swapped for a link
	// after the checks is not followed, and one swapped before refuses the
	// run, as does a kernel or seccomp filter without openat2.
	// No descriptor a caller left open reaches bubblewrap or the program,
	// but the binds' (sandbox.CloseInherited, L4).
	if err := sandbox.CloseInherited(); err != nil {
		leave()
		fmt.Fprintf(os.Stderr, "jhon sandbox: the descriptors this command inherited cannot be closed for the program (%v), so nothing was run; fix: run it again from a shell that leaves no descriptor open\n", err)
		return sandboxRefused, nil
	}
	// The project's top is checked against where it was taken from, the
	// working folder or the --project value as written (L4).
	in.ProjectFrom = "."
	if pinned != "" {
		in.ProjectFrom = pinnedAs
	}
	bound, p := sandbox.Open(in)
	if p != nil {
		leave()
		fmt.Fprintf(os.Stderr, "jhon sandbox: %s, so nothing was run; fix: %s\n", p.Reason, p.Fix)
		return sandboxRefused, nil
	}
	// The program starts through jhon sandbox enter, which checks inside
	// the sandbox that every bind is the folder checked here, mounted where
	// it was meant to be (sandbox.Bound.Command, L4).
	code, err := sandboxProgram(bwrap, append(append([]string{bwrap}, bound.Args...), bound.Command(program)...), env, profile, sandbox.DeliveryNote(program), sandbox.DeployNote(program, in.DeployTools), refusal, bound)
	bound.Close()
	leave()
	return code, err
}

// runRegistry is the registry of running runs (sandbox.Registry): a folder
// of Jhon's state folder, which the sandbox keeps read-only and refuses to
// run with where a sandboxed command could change it.
func runRegistry() sandbox.Registry {
	return sandbox.Registry{Dir: filepath.Join(filepath.Dir(grant.Dir()), "sandbox-runs")}
}

// enterRegistry records this run of kind with areas (sandbox.Registry.Enter,
// waiting up to sandbox.RegistryWait), and gives the function that removes
// the record; refused is the note when it may not run: a run of the other
// kind that shares its areas still runs (L34, L37), or the registry cannot
// be read or written or its lock stays held for the wait (L36 A), which
// refuses it too (fail closed).
func enterRegistry(kind string, areas []string) (leave func(), refused string) {
	r := runRegistry()
	leave, err := r.Enter(kind, areas, sandbox.RegistryWait)
	var busy sandbox.ErrBusy
	switch {
	case err == nil:
		return leave, ""
	case errors.Is(err, sandbox.ErrLocked):
		return nil, "jhon sandbox: the record of running commands (" + r.Dir + ") is locked by another process past " + sandbox.RegistryWait.String() + ", so whether a sandboxed command could change what this runs is unknown, and nothing was run; fix: run it again; if it stays locked, find the process that holds " + filepath.Join(r.Dir, ".lock") + " (fuser " + filepath.Join(r.Dir, ".lock") + ") and end it"
	case errors.As(err, &busy) && kind == sandbox.KindOutside:
		return nil, "jhon sandbox: a sandboxed command that can write what this line reads is still running (pid " + strconv.Itoa(busy.Run.PID) + "), and a line that runs outside the sandbox does not run while one does: that command could change what the line reads (the repository's git configuration, a program it runs), so nothing was run; fix: wait for that command to end, or stop it, then run the line again"
	case errors.As(err, &busy):
		return nil, "jhon sandbox: a line that runs outside the sandbox and reads what this command can write is still running (pid " + strconv.Itoa(busy.Run.PID) + "), and no sandboxed command that can write what it reads starts while it runs, so nothing was run; fix: wait for that line to end, then run the command again"
	}
	return nil, "jhon sandbox: the record of running commands (" + r.Dir + ") cannot be read or written (" + err.Error() + "), so whether a sandboxed command could change what this runs is unknown, and nothing was run; fix: make " + r.Dir + " a folder you can write"
}

// runProblem is what `jhon sandbox run` checks before it runs anything in
// the sandbox of in: a bubblewrap it may trust (sandbox.Check), then Jhon's
// own files (ownFilesProblem). It gives that bubblewrap, and why nothing
// runs, nil when nothing stands in the way. Status asks it too, so it says
// a missing sandbox.allow_write folder is created only when a run would
// create it (L26 A2).
func runProblem(in sandbox.Input) (string, *sandbox.Problem) {
	bwrap, p := sandbox.Check(sandbox.ThisHost(), in)
	if p == nil {
		p = ownFilesProblem(in)
	}
	return bwrap, p
}

// ownFilesProblem is why the sandbox of in runs nothing while a sandboxed
// command could change Jhon's settings file or the state folder with the
// grants and the modes, wherever they are (sandbox-deploy-tools S9, S10):
// it could turn the sandbox off or grant itself what the guards ask for.
// nil when it could change neither.
func ownFilesProblem(in sandbox.Input) *sandbox.Problem {
	var p *sandbox.Problem
	if os.Getenv("JHON_SETTINGS") != "" {
		p = sandbox.SettingsFile(in, settings.Path())
	} else {
		p = sandbox.ConfigSettings(in, settings.Path(), xdgVariable("XDG_CONFIG_HOME"))
	}
	if p == nil {
		p = sandbox.StateFolder(in, filepath.Dir(grant.Dir()), xdgVariable("XDG_STATE_HOME"))
	}
	if p == nil {
		p = sandbox.KeptProblem(in, keptOwn(in))
	}
	return p
}

// keptOwn are Jhon's own paths a sandbox.allow_write folder must not let a
// sandboxed command replace (sandbox.KeptProblem, L28): the release folder
// (under the home, and under $XDG_DATA_HOME) and the folder its `current`
// leads to, each `jhon` program in a PATH folder outside the project and
// the default ~/.local/bin/jhon, each with where it leads, the source
// checkout (sourceCheckout) and the protected paths (in.Protected).
func keptOwn(in sandbox.Input) []sandbox.Kept {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		data = filepath.Join(in.Home, ".local", "share")
	}
	var kept []sandbox.Kept
	add := func(what, p string) {
		if filepath.IsAbs(p) {
			kept = append(kept, sandbox.Kept{What: what, Path: p})
			if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
				kept = append(kept, sandbox.Kept{What: what, Path: real})
			}
		}
	}
	for _, share := range []string{release.Locate().Share, filepath.Join(data, "jhon")} {
		add("Jhon's release folder", share)
		add("Jhon's current release", filepath.Join(share, "current"))
	}
	add("the jhon program", filepath.Join(in.Home, ".local", "bin", "jhon"))
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) || slices.ContainsFunc(in.Roots, func(r string) bool { return project.Within(dir, r) }) {
			continue
		}
		if fi, err := os.Stat(filepath.Join(dir, "jhon")); err == nil && !fi.IsDir() {
			add("the jhon program", filepath.Join(dir, "jhon"))
		}
	}
	add("Jhon's source checkout", sourceCheckout(in))
	for _, p := range in.Protected {
		add("one of Jhon's protected paths", p)
	}
	return kept
}

// xdgVariable is name when the XDG variable moves Jhon's folder (an
// absolute path, as settings.Path and grant.Dir take it), else "".
func xdgVariable(name string) string {
	if filepath.IsAbs(os.Getenv(name)) {
		return name
	}
	return ""
}

// sandboxInput is the sandbox of a program started here, in the pinned
// project or with "" the one this folder is in (sandboxInputAt).
func sandboxInput(pinned string) sandbox.Input {
	return sandboxInputAt(cwd(), pinned)
}

// sandboxInputAt is the sandbox of a program started in dir, in the pinned
// project or with "" the one dir is in: the project's areas, and what the
// settings and the integrations add. Jhon's own files are named by the code
// that owns each: the settings file (and its folder, unless JHON_SETTINGS
// names a file elsewhere), the state folder that holds the grants and
// the modes, and Jhon's source checkout (sourceCheckout). The guard hook
// probes with the same one (hook.SandboxInput): telemetry is an
// integration the hooks never import (T55).
func sandboxInputAt(dir, pinned string) sandbox.Input {
	in := sandbox.Current(dir, pinned)
	in.AllowWrite = settings.Paths("sandbox.allow_write")
	in.DeployTools = settings.Names("sandbox.deploy_tools")
	in.Protected = []string{settings.Path(), filepath.Dir(grant.Dir())}
	if os.Getenv("JHON_SETTINGS") == "" {
		in.Protected = append(in.Protected, filepath.Dir(settings.Path()))
	}
	if source := sourceCheckout(in); source != "" {
		in.Protected = append(in.Protected, source)
	}
	in.Telemetry = telemetry.Dir()
	in.Registry = runRegistry().Dir
	in.Docker = settings.On("sandbox.docker")
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
//
// The project's code reads the folder through every link, so a concurrent
// run that swaps a folder on the way at that moment names another one, the
// home's (T248 L4's second verify): the answer is taken only when it is,
// by device and inode, this folder or one above it (sandbox.HereProject),
// read again a few times, else none, so pi and `jhon start` pin nothing
// rather than a folder the swap chose.
func sandboxProject() string {
	for range 5 {
		if top := project.ReadRepo(cwd()).Top; sandbox.HereProject(top) {
			return top
		}
	}
	return ""
}

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
	// Inside is a status run inside Jhon's sandbox (sandbox.Inside, T217
	// S4), where no other sandbox can start: nothing is probed, CanRun is
	// true, and a default mode reads as on, as the probe outside found it.
	Inside  bool   `json:"inside"`
	Reason  string `json:"reason"`
	Fix     string `json:"fix"`
	Project string `json:"project"`
	// TimedOut is a probe that did not end in time: no verdict (L1382).
	// CanRun stays false, truthfully, and pi wraps the command as usual,
	// as the Claude Code wrap does (L1383).
	TimedOut bool `json:"timed_out"`
	// BashRules is set with the mode on only: with it off the rules apply.
	BashRules *claudeBashRules `json:"claude_bash_rules,omitempty"`
	// MissingAllowWrite are the sandbox.allow_write folders that do not
	// exist and no writable area holds: whether the next sandboxed command
	// makes each, or why Jhon will not (sandbox.MissingAllowWrite, T239).
	MissingAllowWrite []sandbox.MissingFolder `json:"missing_allow_write,omitempty"`
	// UnusedWriteAreas are the writable areas the sandbox does not bind as
	// each is or passes through a link (sandbox.UnusedWriteAreas, L28).
	UnusedWriteAreas []sandbox.Unused `json:"unused_write_areas,omitempty"`
	// RunRefused is why `jhon sandbox run` runs nothing here though the
	// sandbox can run, on one line with the fix: Jhon's own files where a
	// sandboxed command could change them (ownFilesProblem); "" when none.
	RunRefused string `json:"run_refused,omitempty"`
	// AcceptedEscapes are the ways out of the sandbox the owner's settings
	// open on purpose (L37): the docker socket with sandbox.docker on.
	AcceptedEscapes []string `json:"accepted_escapes,omitempty"`
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
	if s.Inside {
		return "sandbox " + s.Mode + "; this command runs inside Jhon's sandbox, where another cannot start, so nothing was probed; jhon sandbox status run outside it (the owner's terminal) gives the real state"
	}
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
// Inside Jhon's sandbox (sandbox.Inside), where bubblewrap cannot start
// another, nothing is probed: the sandbox runs, and the mode is the
// owner's or, by default, on (T217 S4).
func sandboxNow(pinned string) sandboxState {
	s := sandboxState{CanRun: true, Project: pinned}
	if pinned == "" {
		s.Project = sandboxProject()
	}
	in := sandboxInput(pinned)
	s.MissingAllowWrite = sandbox.MissingAllowWrite(in)
	s.UnusedWriteAreas = sandbox.UnusedWriteAreas(in)
	if in.Docker {
		s.AcceptedEscapes = append(s.AcceptedEscapes, sandbox.DockerEscape)
	}
	if p := ownFilesProblem(in); p != nil {
		s.RunRefused = p.Line()
	}
	if slices.ContainsFunc(s.MissingAllowWrite, func(m sandbox.MissingFolder) bool { return m.Create }) {
		if _, p := runProblem(in); p != nil {
			for i, m := range s.MissingAllowWrite {
				if m.Create {
					s.MissingAllowWrite[i].Create = false
					s.MissingAllowWrite[i].Why = "Jhon creates it only for a sandboxed command that runs, and none runs here: " + p.Reason + "; fix: " + p.Fix
				}
			}
		}
	}
	if sandbox.Inside() {
		s.Inside, s.Mode, s.ModeSource = true, "on", sandbox.ModeDefault
		if mode, ok := sandbox.OwnerMode(); ok {
			s.Mode, s.ModeSource = mode, sandbox.ModeSettings
		}
	} else {
		e := sandbox.Mode(sandboxHost(), func() sandbox.Input { return in })
		s.Mode, s.ModeSource = e.Mode, e.Source
		if e.Mode == "off" && e.Problem != nil {
			s.ModeReason = e.Problem.Line()
		}
		// One empty sandbox started: the mode's probe when it probed.
		p := e.Problem
		if !e.Probed {
			p = sandbox.Probe(sandboxHost(), in)
		}
		if p != nil {
			s.CanRun, s.Reason, s.Fix, s.TimedOut = false, p.Reason, p.Fix, p.TimedOut
		}
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
	for _, m := range s.MissingAllowWrite {
		where += "; " + missingAllowWriteLine(m)
	}
	for _, u := range s.UnusedWriteAreas {
		where += "; " + u.Line()
	}
	if s.RunRefused != "" {
		where += "; " + runRefusedLine + s.RunRefused
	}
	for _, e := range s.AcceptedEscapes {
		where += "; " + e
	}
	fmt.Println(s.line() + where)
	return 0, nil
}

// missingAllowWriteLine is what status and doctor say of a
// sandbox.allow_write folder that does not exist (T239).
func missingAllowWriteLine(m sandbox.MissingFolder) string {
	if m.Create {
		return "sandbox.allow_write folder " + m.Folder + " does not exist: the next sandboxed command creates it"
	}
	return "sandbox.allow_write folder " + m.Folder + " does not exist, and Jhon will not create it: " + m.Why
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
	case s.Mode == "on" && s.TimedOut:
		d.o.Warn(s.line() + "; the model's commands are wrapped as usual, and fail with bubblewrap's message if it cannot start")
	case s.Mode == "on":
		d.o.Bad(s.line() + "; until then every command of the model is refused")
	default:
		d.o.Warn(s.line() + "; the model's commands run unsandboxed")
	}
	if w := s.BashRules.warning(); w != "" {
		d.o.Warn(w)
	}
	for _, m := range s.MissingAllowWrite {
		if !m.Create {
			d.o.Warn(missingAllowWriteLine(m))
		}
	}
	for _, u := range s.UnusedWriteAreas {
		d.o.Warn(u.Line())
	}
	for _, e := range s.AcceptedEscapes {
		d.o.Warn(e)
	}
	if s.RunRefused != "" {
		if s.Mode == "on" {
			d.o.Bad(runRefusedLine + s.RunRefused + "; until then every command of the model is refused")
		} else {
			d.o.Warn(runRefusedLine + s.RunRefused)
		}
	}
}

// runRefusedLine opens what status and doctor say of a run refused for
// Jhon's own files (sandboxState.RunRefused).
const runRefusedLine = "jhon sandbox run runs nothing here: "

// mergeGateEntries are the merge gate's entries for an unsandboxed git line
// (hook.MergeGateUnsandboxed, T219 S5) in the repository of the pinned
// project, else of the working directory: none outside a repository under
// LDD.
func mergeGateEntries(pinned string) [][2]string {
	dir := pinned
	if dir == "" {
		dir = cwd()
	}
	return hook.MergeGateUnsandboxed(project.Root(dir))
}
