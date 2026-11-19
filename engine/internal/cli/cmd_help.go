package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// out is the CLI's output for this process: coloured when stdout is a
// terminal.
func out() *release.Out { return release.NewOut(isTerminal(os.Stdout)) }

// usageError is the CLI's usage error: `jhon: <msg>`, a blank line, and
// the usage, all on stderr; exit 2.
func usageError(msg string) (int, error) {
	o := out()
	fmt.Fprintf(o.E, "%sjhon:%s %s\n\n", o.R, o.Z, msg)
	help(o.E, o)
	return 2, nil
}

// unknownCommand is the CLI's answer to a command it does not dispatch:
// the usage on stderr, exit 1.
func unknownCommand(name string) (int, error) {
	o := out()
	fmt.Fprintf(o.E, "%sjhon:%s unknown command: %s\n\n", o.R, o.Z, name)
	help(o.E, o)
	return 1, nil
}

// help is `jhon help`: the usage, then the two roots, each with a
// leading $HOME written as ~.
// A user, who has only the release (L51), reads a user-facing first line
// and no source line (T10j); the maintainer's checkout keeps both.
func help(w io.Writer, o *release.Out) {
	r := release.Locate()
	text := usage
	if !r.HasSource() {
		text = strings.Replace(text, userTitle[0], userTitle[1], 1)
	}
	text = strings.NewReplacer("{B}", o.B, "{Z}", o.Z).Replace(text)
	if !r.HasSource() {
		fmt.Fprintf(w, "%s\n  runtime: %s\n", text, release.Tilde(r.Runtime))
		return
	}
	fmt.Fprintf(w, "%s\n  runtime: %s\n  source:  %s\n", text, release.Tilde(r.Runtime), release.Tilde(r.Source))
}

// userTitle is the usage's first line, and a user's instead.
var userTitle = [2]string{
	"{B}jhon{Z} — maintenance for the Jhon agent framework\n",
	"{B}jhon{Z} — the Jhon agent framework: set it up, start a session, keep it updated\n",
}

// commandPurpose is the one sentence a user command's own help opens with
// (T10i); its usage rows are the general usage's.
var commandPurpose = map[string]string{
	"setup":      "choose the runtimes and integrations, check what they need, and install them.",
	"start":      "start a Jhon session in Claude Code or pi.",
	"update":     "install the latest Jhon release, verified, or go back to the previous one.",
	"doctor":     "check that the installed Jhon is healthy.",
	"status":     "show where a change stands under LDD.",
	"version":    "print the version of this jhon.",
	"settings":   "read and write the owner's settings.",
	"profile":    "list, show and choose the owner's model profiles.",
	"sandbox":    "run a program in the OS sandbox, or say whether the sandbox can run here.",
	"executions": "record a change's test runs, the evidence its delivery is checked against.",
}

// commonCase is the two lines a command's own help opens with, before its
// name, where one case covers most calls (T146h L4: an agent read
// `jhon executions --help` to learn the two runs of a small change).
var commonCase = map[string]string{
	"executions": "common case: `jhon executions run` while the new test fails (RED),\n" +
		"then `jhon executions run` again once it passes (GREEN); commit, then `jhon deliver check`.\n\n",
}

// commandHelp is `jhon <cmd> --help` for a user command: its name and
// purpose, then every usage row of the general usage that is the command's;
// whether cmd has one.
func commandHelp(w io.Writer, o *release.Out, cmd string) bool {
	purpose, ok := commandPurpose[cmd]
	if !ok {
		return false
	}
	text := strings.NewReplacer("{B}", o.B, "{Z}", o.Z).Replace(usage)
	var rows []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "  jhon "):
			in = strings.HasPrefix(l, "  jhon "+cmd+" ") || l == "  jhon "+cmd
		case !strings.HasPrefix(l, "   "):
			in = false
		}
		if in {
			rows = append(rows, l)
		}
	}
	fmt.Fprintf(w, "%s%sjhon %s%s — %s\n\n%susage%s\n%s\n", commonCase[cmd], o.B, cmd, o.Z, purpose, o.B, o.Z, strings.Join(rows, "\n"))
	return true
}

// usage is the CLI's usage text; {B} and {Z} open and close bold.
const usage = `{B}jhon{Z} — maintenance for the Jhon agent framework

{B}usage{Z}
  jhon <command> --help    the command's own help for setup, start, update,
                           doctor, status, version, settings, profile,
                           sandbox and executions (which opens with its
                           common case), this usage for the others; -h or --help
                           after any command runs nothing (after jhon sandbox
                           run --, it is the program's)
  jhon setup               the setup wizard, on a terminal: pick the runtimes and
                           the opt-in integrations, check git, claude, node,
                           engram and codegraph with the fix for each, then
                           write the settings and install the runtimes from the
                           active release; exit 2 without a terminal
  jhon start [<claude args>]
                           start a Jhon session: claude --agent jhon-orchestrator
                           with the release's plugin, which carries the
                           /jhon-ldd command; every other argument goes to
                           claude; the claude.ai account connectors stay out
                           (ENABLE_CLAUDEAI_MCP_SERVERS=false) unless
                           claude_code.connectors is on or the variable is
                           set; Jhon's MCP servers (jhon, engram, codegraph)
                           stay
                           with -p/--print, where --agent already makes the
                           orchestrator the system prompt, it sets
                           JHON_AGENT_LOADED=jhon-orchestrator and the
                           session's start hands it no second copy
  jhon start pi [<pi args>]
                           start pi on Jhon's agent directory, ~/.pi/jhon (the
                           orchestrator, the agents, and the guards); every
                           other argument goes to pi
                           both set CLAUDE_STREAM_FIRST_BYTE_TIMEOUT_MS=60000
                           (Claude Code retries an API that sends nothing after
                           60 s, not minutes) unless it is already set
                           both set JHON_SANDBOX_PROJECT to the project the
                           session starts in (the folder that holds .git, as
                           jhon sandbox status names it), replacing any value
                           and leaving it unset in no repository: the project
                           every sandboxed command of the session is pinned
                           to. With sandbox.mode on, Claude Code's guard hook
                           (jhon hook pre-tool) answers a Bash or Monitor call
                           the guards did not refuse with their decision
                           (allow or ask) and updatedInput: the same input
                           with its command as <jhon> sandbox run --project
                           '<folder>' --profile work -- <shell> -c
                           '<command>'; without the variable, or where the
                           sandbox cannot run, the call is refused with the
                           fix; pi wraps its own commands. In Claude Code's
                           plan mode an allowed command carries no decision,
                           so plan mode's rules decide. A foreground Bash
                           call's line also moves Claude Code's shell to
                           the directory the command ended in, with the
                           command's exit code, so a cd carries to the next
                           call; a background Bash call and a Monitor
                           command are not followed
  jhon update [--check] [--json]
                           on a terminal, the update screens; otherwise check
                           the latest release of jachavarro2004/jhon-agent
                           and install it when newer, after its sha256 and its
                           cosign keyless signature (the release workflow)
                           verify; a failure changes nothing; --check only
                           reports; exit 0 up to date, updated, or no stable
                           release yet (--json status no-release), 1 failed;
                           a development build (0.0.0-dev, from jhon sync)
                           installs nothing and says to run jhon sync: exit 2,
                           0 with --check or --json ("dev": true)
  jhon update --rollback   re-activate the previous release kept on disk; after
                           an update or a rollback, a running dashboard proven
                           Jhon's from another release is restarted on its port
  jhon doctor [--remote]   every health check in one command; no network call
                           unless --remote, which compares origin and mirror
                           with HEAD through ls-remote and asks context7's
                           hosted endpoint whether it answers (a warning
                           when it does not; without --remote the row says
                           how it is mounted, or that it is off); warns when
                           claude, engram or codegraph differs from
                           the version Jhon pins, and where the OS sandbox
                           cannot run with sandbox.mode off (the model's
                           commands run unsandboxed: macOS and Windows by
                           default, and Linux by default where no empty
                           sandbox starts, with the fix that turns the
                           sandbox on), or on Linux the owner set it off
                           (the shell's writes outside the project then run
                           unguarded); fails where it cannot run with the
                           mode set on (every command is refused), with the
                           fix; warns, never fails, when the probe does not
                           end in time (commands are wrapped as usual);
                           fails when the record of the
                           rendered agents or the main agent's definition
                           is missing from ~/.claude/agents, or the
                           definition cannot be read or is empty
  jhon doctor --runtime <name>
                           one runtime's guarantee level per behavioural rule
                           and the verification state of each primitive, with
                           its contract bundle against its instruction budget;
                           no global score; exit 1 when the bundle does not fit
                           or the matrix fails for that runtime; for pi, the
                           provider the active profile runs on (claude needed
                           only for claude-bridge); a runtime turned off in
                           settings.runtimes is said off, nothing checked
  jhon status [change] [--json | --brief]
                           informational LDD state of a change: artifacts,
                           tasks, the last test run, notes, and an advisory
                           next step; never a gate (read-only); a change owes
                           only what it needs (tasks.md, a spec delta) and
                           always its RED and GREEN runs; with no folder, the
                           change is the one its branch names; an umbrella
                           (only an exploration.md, or kind: umbrella in its
                           .openspec.yaml) is selected only when named, listed
                           apart, its next step to split it; several
                           left undecided are ranked by pending work, none
                           selected
  jhon version [--json]    the version stamped into this binary, its commit and
                           date (0.0.0-dev when unstamped); also jhon --version
  jhon settings [list] [--json]
                           the owner's settings (~/.config/jhon/settings.toml,
                           schema 1; JHON_SETTINGS names another file): every
                           key with its value and source (default, file, env;
                           --json adds its kind, accepted values and default),
                           then the project's openspec/config.yaml rules
                           (project, read-only); the first read migrates the
                           active profile from profiles.json, with a backup; a key
                           a release removed is warned on stderr with why
  jhon settings get <key> [--json]
  jhon settings set <key> <value>
                           write one key, keeping the file's comments; a value
                           the schema refuses exits 2 with what it accepts;
                           integrations.engram and codegraph stay true (required);
                           telemetry is opt-in (off by default);
                           integrations.context7 (on by default) mounts
                           context7's hosted documentation server
                           (https://mcp.context7.com/mcp, no key, nothing
                           installed) for the explore and worker agents:
                           a call sends the library's name or id and the
                           question asked; false takes the server, its
                           tools and its sentence out of the render (the
                           next sync applies it);
                           claude_code.connectors (off by default) lets
                           jhon start load the claude.ai connectors;
                           agents.act_on_announce (on by default) sends
                           a main session's turn back once when its
                           reply announces an action it did not take
                           ("sigo con…", "I'll now…"), to do it or say
                           why not; false lets such a turn end;
                           theme.palette is rose-ink (default) or
                           desktop, the palette file JHON_DESKTOP_COLORS
                           names (ryoku, its older name, still works);
                           sandbox.mode is on or off: whether the
                           runtimes run each command of the model in the
                           OS sandbox, on by default where the sandbox can
                           run (Linux, where one empty sandbox starts;
                           elsewhere off: the commands run unsandboxed, and
                           jhon doctor warns with the fix);
                           sandbox.allow_write is
                           the folders such a command may write besides the
                           project's, separated by commas, each absolute or
                           under ~/, never /, the home folder or one above
                           it (an empty list is written in the file: []).
                           Both are the owner's: the file is read-only
                           inside the sandbox.
                           jhon setup sets the runtimes
                           and integrations, /jhon:customize the shell.* keys
  jhon settings migrate [--json]
                           the one-time move from profiles.json, then drops
                           the keys v0.1 and v0.2 removed
                           (card_style, quiet_tools, key_hints, [shell.extra],
                           keys.usage, ...), placing renamed values, after a
                           backup to settings-backup-<UTC>/settings.toml
  jhon profile [list] | show [<name>] | use <name>
                           the owner's model profiles (~/.config/jhon/profiles.json):
                           list them with the provider pi runs on, show each
                           agent's model per runtime, or activate one and
                           re-render every runtime. A profile names pi's
                           provider in its pi models (provider/model[@thinking],
                           e.g. tiers.pi.strong = anthropic/claude-opus-4-5);
                           pi-claude-bridge is installed when one is on
                           claude-bridge, the default, or when Claude Code is
                           one of this install's runtimes
  jhon profile mode <profile> <inline|writer>
                           the profile's delegation mode: inline (the
                           default; the orchestrator may write small work) or
                           writer (it reads and plans, workers write);
                           default is always inline
  jhon release install <artifact.tar.gz>
                           verify it against the .sha256 beside it, unpack it
                           into ~/.local/share/jhon/releases/<commit>, then
                           preflight, switch current and install as sync does;
                           no checkout or toolchain needed; a failed check
                           changes nothing, and earlier releases stay
  jhon init [dir] [--json] read-only project discovery for /jhon-ldd init
  jhon context [<change>] --phase <phase|none> [--json] [--root <project>]
                           the launch context of one delegation as one JSON
                           object (read-only, optional); exit 0 whenever it
                           resolves, even below vcs ready, 2 on a usage error;
                           --handoff adds the decisions on record; --judge the
                           tests turned green by editing only their test, which
                           verify and the 4R lenses judge; --save-state
                           --after <commit> (started by route-guard) writes the
                           change's state record to Engram once HEAD moves
  jhon executions run [<change>] [--affected [<base-ref>]] [--timeout <minutes>] [--jobs <n>] [--force] [--dry-run] [--json]
                           with no change named, the one jhon status selects
                           on the branch (its first line says which); run each
                           selected testable project's resolved
                           command in its directory, each under a deadline
                           (default 20 min; a timed-out project is recorded and
                           the run continues), and write the change's record
                           after every project (in its folder, or under the
                           git common dir, jhon/changes/<change>/, for a
                           change with no folder, which its branch's name,
                           feat/x, also names; earlier runs are kept, so a
                           RED before GREEN shows); --affected selects the projects
                           changed since the base, those whose tests name a
                           changed file's whole path or import path, or a
                           changed document, and their dependents (every
                           project, with the reason, when that cannot be
                           told); with no base it is a unit's run, the default
                           for every intermediate pass: what changed since the
                           change's last passing run, else since its base; its
                           record closes no slice and delivers nothing, so the
                           slice base (--affected <base-ref>, or a full run) is
                           for the RED record and the slice's closing run only;
                           a uv-run pytest project is narrowed by
                           pytest-testmon, and one with no record runs whole
                           once to build it (a first run its deadline cut is
                           left to jhon executions prime); a wide one runs only
                           the test modules the change touched, those its
                           record holds narrowed by it;
                           projects run in parallel, --jobs at a time
                           (executions_jobs in openspec/config.yaml, else the
                           CPUs, at most 4); the projects of one
                           executions_exclusive group never run together; a
                           command that passed in the last run on the same
                           working tree is reused unless --force
  jhon executions prime [--timeout <minutes>]
                           build the pytest-testmon record of each uv-run
                           pytest project that has none: its whole test
                           command once, under the deadline, kept when pytest
                           ran to its end; writes no execution record; exit 0
                           when none is missing any more, 1 otherwise
  jhon executions verify <change> --verdict pass|fail [--note <text>] [--json]
                           record a light verify (the verify agent's, when a
                           slice of a code change closes) in the change's
                           record, beside its runs, with HEAD's tree; on the
                           standard route a PR needs a passing one of the
                           tree its last run tested; a pass is refused (exit
                           2) for any change whose runs show no RED before
                           GREEN, whatever its route
  jhon deliver check <change> [--action <merge|deploy|release>] [--artifact <name@sha256:digest|commit>]
                           advisory: ready when the tree is clean and HEAD's
                           tree is the last passing test run's; deploy and
                           release need an immutable artifact digest (only
                           deploy, for infrastructure, may name the full
                           HEAD commit), recorded in delivery.json; exit 0 ready,
                           1 not, 2 usage
  jhon compose <main-spec> <delta-spec>
                           apply a delta spec to a main spec, atomically
  jhon archive <change-name> <YYYY-MM-DD>
                           move a change into openspec/changes/archive, with
                           git mv or a rename, and verify it (project root)
  jhon judgment append <record> <event-file|->
                           validate one judgment event and append it, atomically
  jhon judgment status <record> [--json]
                           derived state and next action of a judgment record
  jhon sessions [--json]   live orchestrator sessions that started before the
                           last install, which should be resumed (read-only)
  jhon grant <class>… [--for <n>{s|m|h|d}] [--root <path>]
                           authorize one or more outward-facing classes (push,
                           gh-write, publish, remote, deploy, outside-write),
                           or destroy (a deletion that loses work:
                           a forced branch delete, reset --hard, docker rm -f),
                           for this project and its worktrees; push is a push
                           to the default branch, of every branch, or of tags
                           (another branch's passes), gh-write a merge, a
                           release, or a repository write (a pull request's
                           create, comment, ready, and edit pass)
                           for a while (default 30m), one expiry for all; run
                           it yourself (with ` + "`!`" + ` in Claude Code, in your own
                           terminal in pi): the safety guard refuses it from
                           an agent
  jhon grant --list | --revoke <class>… [--root <path>]
                           list the grants, or revoke some
  jhon grant [--revoke] outside-write --session <id> [--root <folder>]
                           in a folder that is no git repository (no project:
                           no mode and no grant for a while), let one
                           session's Edit and Write calls under it pass until
                           the session ends (12h at most); never HOME or /, and nothing
                           but outside-write. pi offers it when it asks
                           about a write there and revokes it at the end;
                           in Claude Code a yes covers that one call
  jhon mode [normal|yolo|super-yolo] [--root <path>] [--json]
                           the owner's permission mode for this project and its
                           worktrees, until you set another: yolo passes every
                           class above but destroy and an unreadable command
                           without asking, super-yolo those too, normal asks;
                           Claude Code settings and a git config that changes
                           what git runs ask and a refusal stays one in every
                           mode. With no mode, prints it; setting one is
                           yours alone: run it with ` + "`!`" + ` in Claude Code, or in
                           your own terminal in pi
                           A folder that is no git repository takes no mode:
                           it is refused there, and calls there are judged
                           as in normal mode
  jhon sandbox run [--profile work|delivery] [--project <folder>] -- <program> [args...]
                           run the program under bubblewrap (Linux), in its
                           place: stdin, stdout, stderr, the exit code and
                           signals are the program's. It may write the
                           project (the folder that holds .git, its listed
                           worktrees, <root>-worktrees, and from a linked
                           worktree the git common dir and the main
                           worktree), /tmp and $TMPDIR, the cache (~/.cache,
                           $XDG_CACHE_HOME), the toolchains' caches and
                           stores (go's ~/go/pkg, cargo's registry and git
                           checkouts, ~/.npm, pnpm's store, bun's install
                           cache, ~/.gradle, ~/.m2/repository,
                           ~/.nuget/packages, mise's cache, pip's and uv's
                           caches), sandbox.allow_write and Jhon's telemetry
                           folder; everything else is read-only,
                           Jhon's settings, grants, modes and releases
                           included, wherever they are, Jhon's source
                           checkout unless it is the project (a linked
                           worktree of it writes only its own files and
                           the git common dir), and every PATH
                           folder outside the project, also inside a
                           writable folder: the folders with the
                           toolchains' programs (go's and cargo's bin,
                           rustup, mise's installs and shims) and mise's
                           state, with the configs it trusts, are the
                           owner's to open with sandbox.allow_write. $TMPDIR
                           is bound only under /tmp, /var/tmp, the home or a
                           sandbox.allow_write folder, and not inside a
                           hidden folder or one of Jhon's; any other value
                           becomes /tmp for the program. The project is
                           read from the disk and no git runs:
                           core.worktree, config includes and GIT_*
                           variables are not followed, and a .git file or a
                           listed worktree counts only when the other side
                           points back at it. --project <folder> pins the
                           project instead of walking up from the working
                           directory, which need not be inside it: an
                           absolute folder that exists and is not /, the
                           home or one above it, whose other folders still
                           come from its own .git; a command that removed
                           the project's .git then opens no enclosing
                           repository at the next start. The credentials are not
                           there: ~/.ssh, ~/.gnupg, ~/.aws, ~/.config/gh,
                           ~/.config/gcloud, ~/.netrc, ~/.git-credentials,
                           ~/.docker/config.json, ~/.kube, ~/.npmrc, cargo's
                           token, the runtimes' auth files, ~/.Xauthority,
                           the SSH agent socket, and the snapshot folders
                           (/.snapshots, /home/.snapshots, ~/.snapshots,
                           /timeshift, /run/timeshift). $XDG_RUNTIME_DIR and
                           /run/user/<uid>, whether or not the variable
                           names it, are private empty folders (mode 0700)
                           that show only docker's and podman's sockets, so the
                           session bus, the keyring and the desktop's
                           sockets are not there; nor are tmux's, X11's and
                           ICE's socket folders under /tmp. GH_TOKEN,
                           GITHUB_TOKEN, GH_ENTERPRISE_TOKEN,
                           GITHUB_ENTERPRISE_TOKEN, SSH_AUTH_SOCK,
                           GIT_ASKPASS and SSH_ASKPASS are unset.
                           --profile delivery shows ~/.ssh, ~/.config/gh
                           and the agent socket, read-only, and keeps those
                           variables. No network or process isolation:
                           abstract unix sockets stay reachable (closing
                           them takes a network namespace), and with no PID
                           namespace a child the program detaches (setsid,
                           nohup, a daemon) outlives bubblewrap: the
                           runtime's kill and --die-with-parent end
                           bubblewrap and the program, not that child.
                           When its output is not a terminal (a runtime's
                           call), jhon stays the program's parent: a
                           failure whose output says a path is read-only
                           (Read-only file system) ends with one line on
                           stderr, the sandbox refused a write to <path>;
                           that is a stop: tell the owner the path (jhon
                           settings sandbox.allow_write), never write it
                           elsewhere; and with telemetry on each run
                           records a jhon.sandbox event (its profile, work
                           or unsandboxed, and the exit code). The
                           runtimes' --profile unsandboxed runs <shell> -c
                           <line> with no sandbox when jhon sandbox profile
                           names the line unsandboxed and the shell, git,
                           gh, ssh, docker and docker-compose are not files
                           a sandboxed command may write (the owner's
                           environment kept: DOCKER_HOST, the docker
                           context); anything else runs with the work
                           profile. Its git runs with the repository's
                           hooks, ssh command, fsmonitor and credential
                           helpers overridden (GIT_CONFIG_COUNT entries
                           after the owner's own: core.hooksPath=/dev/null,
                           core.sshCommand=ssh, core.fsmonitor=false,
                           credential.helper reset then the owner's global
                           and system helpers, the ext, fd and file
                           transports off, no submodule recursion, and
                           hook.<event>.enabled=false for every hook event,
                           which also stops hooks named in the
                           configuration, hook.<name>.command), and
                           GIT_CONFIG_PARAMETERS unset. A caller whose
                           stdout and stderr are one file (2>&1) gets the
                           program's output as one stream, in its order.
                           url.*.insteadOf and
                           include.* in the repository's configuration are
                           not overridden: an accepted residual risk. In the
                           work profile uv's tools go to
                           $XDG_CACHE_HOME/jhon/sandbox/uv-tools (UV_TOOL_DIR
                           and UV_TOOL_BIN_DIR, unless already set);
                           ~/.local/share/uv stays read-only. bubblewrap is
                           taken only from a PATH folder outside the
                           project and every folder the sandbox opens for
                           writing. It always sandboxes, whatever
                           sandbox.mode says: where no such bubblewrap or
                           no user namespaces are there, or on macOS and
                           Windows, it runs nothing, says why and the fix
                           on one line, and exits 126; 2 on a usage error
  jhon sandbox profile [--project <folder>] -- <command>
                           how the runtimes run a command line with
                           sandbox.mode on: unsandboxed when every program
                           on it is git with a network subcommand (fetch,
                           pull, push, ls-remote, clone, remote update) and
                           only listed options, working inside the project
                           (the working directory, its -C folders and a
                           clone's new folder), or gh with a listed
                           subcommand (not auth, extension, alias, config
                           or a name gh does not know) that runs no git,
                           reads no local file and downloads nothing, or
                           docker or docker-compose (the owner's deployment
                           tool, volume mounts not judged) but docker
                           login, logout and context create, use, rm,
                           update, import and export, and the line
                           redirects to no file; work for any other line,
                           and for one it cannot read or whose words the
                           shell would still rewrite (a $, a backtick, an
                           unquoted glob or brace; a single-quoted word,
                           or a double-quoted one with no $ or backtick, is
                           literal: --format '{{.Names}}'). git over SSH,
                           gh and the owner's docker cannot run inside the
                           sandbox; the guards judge these lines as any
                           other.
                           --project as for run. exit 0, 2 on a usage
                           error
  jhon sandbox status [--json] [--project <folder>]
                           sandbox.mode (on or off: whether the runtimes
                           sandbox the model's commands; on by default on
                           Linux where one empty sandbox starts, off
                           elsewhere), whether the sandbox
                           can run here (one empty sandbox is started with
                           the bubblewrap and the options run uses, so one
                           older than 0.5.0 cannot, as for jhon doctor), and
                           the reason and the fix when it cannot; a probe
                           that does not end within 5 s is no verdict: the
                           mode stays on and commands are wrapped as usual,
                           failing with bubblewrap's message if it cannot
                           start; and the
                           project a run from here gets (the folder that
                           holds .git, or none in no repository; with
                           --project, that folder); --json: mode,
                           mode_source (default or settings), mode_reason
                           (why the default is off on Linux: the reason
                           and the fix, else ""), can_run, reason, fix,
                           timed_out (the probe did not end in time;
                           can_run stays false), project ("" for none);
                           --project as for run; exit 0, 2 on a usage
                           error. With
                           sandbox.mode on it also counts the Bash deny and
                           ask rules in Claude Code's settings (the user's
                           ~/.claude/settings.json or CLAUDE_CONFIG_DIR's,
                           and the project's .claude/settings.json and
                           .claude/settings.local.json): Claude Code matches
                           them against the wrapped command, so they do not
                           apply to sandboxed commands; the line ends with a
                           warning when there are any or a file could not
                           be read, never with a rule's text, jhon doctor
                           warns the same, and --json has claude_bash_rules:
                           deny, ask, unreadable
  jhon review freeze [--base <ref>] [--json]
                           a review's frozen candidate: the merge base of HEAD
                           and the base, the working tree as a git tree, and
                           the subject hash of their diff; the same bytes give
                           the same subject, so each lens freezes for itself
                           An empty diff is refused (exit 2) with the
                           repository's worktrees that hold changes: freeze
                           from the change's worktree
  jhon review start [--base <ref>] [--change <name>] [--json]
                           the native 4R: one freeze (the change's own records
                           left out of the subject) and, per lens the diff
                           calls for, a tool-less prompt with the lens's
                           instructions, the diff and every changed file
                           inline, its routing key review-<lens>, the active
                           profile's pi model and thinking (model_source:
                           profile-agent, profile-tier or agents.yaml), its bytes and
                           estimated tokens; a prompt over ~100K tokens is
                           refused (exit 1): split the slice
  jhon review refute --subject <hash> < lens-results.json
  jhon review validate --subject <hash> < refuted.json
                           the refuter's prompt for the lens findings, and the
                           validator's for those the refuter did not refute,
                           with its routing key (review-refuter,
                           review-validator) and model, effort and
                           model_source as start gives each lens
  jhon review record --subject <hash> < results.json
                           checks the lens, refuter and validator answers,
                           reduces them and writes the change's review-4r.md
                           with lenses:, verdict:, subject: and base:; it holds
                           while a fresh freeze gives that subject
                           Each refuses (exit 1) a subject the candidate no
                           longer freezes to, and an answer it cannot read,
                           writing nothing
  jhon review run [--base <ref>] [--change <name>] [--timeout <seconds>]
                           the native 4R in one call, for Claude Code: start,
                           each lens prompt through claude -p (no tools, one
                           turn, no session, no settings, the profile's Claude
                           Code model and effort for its routing key) all at
                           once, then the refuter and validator the same way,
                           then record; a failed
                           call (no claude, a non-zero exit, output that is
                           not its JSON, a timeout), SIGINT or SIGTERM stops
                           every call, records nothing, exits 1 and
                           names the review-* lens agents as the fallback
  jhon audit --all [--since <n>d | <YYYY-MM-DD>] [--json] [--draft [<dir>] [--min <n>] [--refresh]]
                           every Claude Code session and Jhon pi session changed
                           since then (default 30d), audited in parallel and
                           aggregated: findings by check and severity, Jhon's
                           sessions apart, each sub-agent type's turns against
                           its mark (its cap, or 100 turns) and its token share
                           past it, each agent role's use of memory, CodeGraph
                           and the todo list summed, Jhon's sessions apart,
                           and the owner's dialogue summed, Jhon's sessions
                           apart; --draft: the findings repeated at
                           least --min times (default 2), grouped by check,
                           anchor, runtime, agent type and cause, one
                           <check>-<key>.md each under <dir> (default
                           $XDG_STATE_HOME/jhon/audit-drafts); an existing
                           draft is kept, --refresh rewrites only its counts
                           and its Evidence
  jhon audit <session-id|path> [--json | --html] [--context]
                           audit one Claude Code session's transcripts against
                           the behavioural rules (read-only); exit 0 clean or
                           suspect-only, 1 on a violation, 2 on an audit-error;
                           beside the timing it states the owner's dialogue
                           with the main thread, as statistics and never a
                           finding: the owner's answers, the responses, tool
                           calls and time each took until the session next
                           waited for the owner (median and maximum), the
                           ask-tool calls by their questions (1, 2, 3, 4 or
                           more), the turns that end in a question in
                           plain text with no ask-tool call (a lower bound),
                           and the owner's corrections: of the messages the
                           owner wrote, those that correct the agent, counted
                           by kind, keep-going ("sigue", "no pares", "por qué
                           paraste", "keep going", "don't stop"), never in a
                           message that answers a question (the last reply
                           ended in a question mark or called the ask tool),
                           repeat ("te dije", "otra vez", "te repito", "I told
                           you"), unrequested ("no te pedí", "eso no",
                           "sobreingeniería", "no hagas eso", "I didn't ask")
                           and decision ("no decidas", "pregúntame", "esa
                           decisión es mía", "ask me"), accents optional;
                           and, as statistics too, how each agent role (the
                           main thread as the orchestrator, each sub-agent by
                           its type) used the integrations: its launches, those
                           with a memory read and with a memory write, its
                           CodeGraph calls against its plain reads (read, grep,
                           find, ls, and a shell command whose first program,
                           after an optional "cd <dir> &&", is cat, sed -n,
                           head, tail, grep, rg, ls or find) with CodeGraph's
                           share, and its todo calls; --context prints the
                           context-use measurement; --html
                           prints the session as one self-contained page of the
                           dashboard component; --json and --view are cached in
                           the user cache dir (jhon/audit), keyed by the session
                           and every input the audit reads: a stale input means
                           a new audit
  jhon telemetry [--days <n>] [--json]
                           what Jhon recorded of its own decisions and runs in
                           the last n days (default 7, at most 30): each hook's
                           decisions and durations, the commands, test runs and
                           delivery checks, the sub-agents and how they ended,
                           the compactions, the latest refusals (read-only);
                           recorded only with integrations.telemetry on
                           (JHON_TELEMETRY=on|off overrides it)
  jhon mcp                 a read-only MCP server over stdio: audit and status;
                           the plugin jhon start loads runs it
  jhon dashboard [--port <n>]
                           a read-only dashboard on http://127.0.0.1:7480: the
                           release's dashboard component over the recent
                           sessions of every project, each one's audit built
                           when opened, and a Changes page; keys: j/k, Enter,
                           Esc, g h/s/c/u/m/t, ctrl+k to find, ? for all; Ctrl-C
                           stops it. On a port held by a dashboard proven
                           Jhon's, one from another release is replaced, one
                           from this release left to answer; anything else
                           is left running and said, exit 1
  jhon mutate <file> --find <text> --replace <text> -- <test command…>
                           the mutation witness: replace the one occurrence,
                           run the test, restore the file byte for byte, run it
                           again; exit 0 when the mutant fails and the restored
                           code passes, 1 otherwise, 2 on a usage error
  jhon mutate --restore    write back a mutation a killed run left in place
  jhon risk [--base <ref>] [--json]
                           the risk tier (passive, medium, high) of the change
                           against the default branch, with a reason per file;
                           openspec/config.yaml risk_rules overrides (read-only)
  jhon contract [--json]   the shell contract jhon-shell (shell/) must speak

{B}maintainer (needs the source checkout){Z}
  jhon validate            contracts are internally consistent
  jhon check               + rendered artifacts match the repo
  jhon sync                build a release from the committed HEAD, preflight it
                           (validation, settings.json without disableAllHooks),
                           switch ~/.local/share/jhon/current to it, install the
                           runtime projections from it; it pushes nothing and
                           takes no argument. It records the checkout it ran
                           from in ~/.cache/jhon/install.json: the installed
                           jhon works on that source outside a checkout unless
                           JHON_SOURCE names another. Only the runtimes on in
                           settings.runtimes (claude_code, pi) are installed and
                           checked; one off is skipped and its directory left
                           as is. A running dashboard proven Jhon's from
                           another release is restarted from the new one on
                           its port
  jhon publish             push the current branch to origin and mirror; refused
                           unless the tree is clean and HEAD is the active
                           release; it takes no argument
  jhon release pack [--version <v>] [--out <dir>]
                           the artifact of the committed HEAD, Go tests passed:
                           jhon-<v>-linux-<arch>.tar.gz (the archived HEAD and
                           build/jhon stamped <v>, by default
                           0.0.0-dev+<commit>) and its .sha256, in --out
                           (default .)
  jhon test [filter]       unit tests, then the rule coverage matrix

{B}notes{Z}
  init writes nothing. It reports the workspace root, the discovered
  projects with their test commands, whether openspec/config.yaml exists,
  and the root's VCS state; /jhon-ldd init is the only writer of that file. The framework's
  skills and agents are global and are never installed into a project.
`
