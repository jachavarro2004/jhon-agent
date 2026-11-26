package sh

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// External runners that run the command after their own options (T228 L2):
// each is read through its option grammar, and the command it runs is read
// as it is read bare. An option a grammar does not know leaves the command
// unknown, so the line is unreadable, never safe.

// A grammar is a runner's options, as getopt reads them.
type grammar struct {
	// short are its short options with no value; valued, those that take
	// one (the rest of the word, else the next word); optional, those
	// whose value, if any, is the rest of the word; none, those after
	// which it runs no command (a pid mode, a listing, the help).
	short, valued, optional, none string
	// long are its long options and the words of value each takes when
	// it is not given after `=`; -1 is one after which no command runs.
	long map[string]int
	// oneWord is a runner whose every option is one word (--name=value).
	oneWord bool
	// operands are the words between its options and the command:
	// chrt's priority, taskset's mask, chroot's root, gosu's user.
	operands int
}

// lo is a long-option table: options with no value, with one, and those
// after which no command runs.
func lo(zero, one, none string) map[string]int {
	m := map[string]int{}
	for _, n := range strings.Fields(zero) {
		m[n] = 0
	}
	for _, n := range strings.Fields(one) {
		m[n] = 1
	}
	for _, n := range strings.Fields(none) {
		m[n] = -1
	}
	return m
}

var runnerGrammars = map[string]grammar{
	"setsid": {short: "cfw", none: "hV", long: lo("--ctty --fork --wait", "", "--help --version")},
	"ionice": {short: "t", valued: "cn", none: "pPuhV", long: lo("--ignore", "--class --classdata", "--pid --pgid --uid --help --version")},
	"chrt": {short: "abdefiorRv", valued: "TPD", none: "pmhV", operands: 1,
		long: lo("--all-tasks --batch --deadline --ext --fifo --idle --other --rr --reset-on-fork --verbose",
			"--sched-runtime --sched-period --sched-deadline", "--pid --max --help --version")},
	"taskset": {short: "ac", none: "phV", operands: 1, long: lo("--all-tasks --cpu-list", "", "--pid --help --version")},
	"strace": {short: "AcCdDfFiknqrtTvwxyYzZ", valued: "abeEIoOpPsSuUX", none: "hV",
		long: lo("--follow-forks --output-separately --summary-only --summary --seccomp-bpf",
			"--output --trace --signal --status --attach --user --env --string-limit", "--help --version")},
	"ltrace":   {short: "bcCfiLrStT", valued: "aADeFlnopsuwxX", none: "hV", long: lo("", "", "--help --version")},
	"unbuffer": {short: "p"},
	"chroot":   {operands: 1, long: lo("--skip-chdir", "--userspec --groups", "--help --version")},
	"setpriv": {none: "dLhV", long: lo("--clear-groups --keep-groups --init-groups --nnp --no-new-privs --reset-env",
		"--reuid --regid --ruid --rgid --euid --egid --groups --inh-caps --ambient-caps --bounding-set --securebits --pdeathsig "+
			"--selinux-label --apparmor-profile --landlock-access --landlock-rule", "--dump --list-caps --help --version")},
	"nsenter": {short: "aeFZc", optional: "muinpUCTrw", valued: "tSGW", none: "hV",
		long: lo("--all --mount --uts --ipc --net --pid --user --cgroup --time --root --wd --env --no-fork --follow-context "+
			"--preserve-credentials --keep-caps --join-cgroup --user-parent", "--target --setuid --setgid --wdns", "--help --version")},
	"unshare": {short: "frc", optional: "muinpUCT", valued: "RwSGl", none: "hV",
		long: lo("--mount --uts --ipc --net --pid --user --cgroup --time --fork --kill-child --mount-proc --map-root-user "+
			"--map-current-user --map-auto --keep-caps", "--propagation --setgroups --map-user --map-group --map-users --map-groups "+
			"--monotonic --boottime --root --wd --setuid --setgid --load-interp", "--help --version")},
	"firejail": {oneWord: true},
	"valgrind": {oneWord: true},
	"bwrap":    {long: bwrapLong},
	"cgexec":   {valued: "g", none: "h", long: lo("--sticky", "", "")},
	"numactl": {short: "abl", valued: "imNCpPw", none: "sHhV", long: lo("--all --balancing --localalloc",
		"--interleave --membind --cpunodebind --physcpubind --preferred --preferred-many --weighted-interleave",
		"--show --hardware --help --version")},
	"prlimit": {optional: "AcdefilmnqrstuvxyR", valued: "o", none: "phV",
		long: lo("--as --core --cpu --data --fsize --locks --memlock --msgqueue --nice --nofile --nproc --rss --rtprio --rttime "+
			"--sigpending --stack --noheadings --raw --verbose", "--output", "--pid --help --version")},
	"faketime": {short: "mf", valued: "p", operands: 1, long: lo("--exclude-monotonic --disable-shm", "--date-prog", "--help --version")},
	"xvfb-run": {short: "adl", valued: "efnpsw", none: "h", long: lo("--auto-servernum --auto-display --listen-tcp",
		"--error-file --auth-file --server-num --xauth-protocol --server-args --wait", "--help")},
	"catchsegv": {},
	"eatmydata": {},
	"fakeroot":  {short: "u", valued: "lsib", none: "hv", long: lo("--unknown-is-real", "--lib --faked", "--help --version")},
	"gosu":      {operands: 1},
	"su-exec":   {operands: 1},
	"setuidgid": {operands: 1},
	"chronic":   {short: "ev"},
	"ifne":      {short: "n"},
	"lckdo":     {short: "qw", operands: 1},
	"systemd-run": {short: "rdtPqG", valued: "upEHM", none: "Sh",
		long: lo("--no-ask-password --scope --slice-inherit --remain-after-exit --send-sighup --same-dir --pty --pipe --quiet "+
			"--on-clock-change --on-timezone-change --no-block --wait --collect --user --system --verbose",
			"--unit --property --description --slice --service-type --uid --gid --nice --working-directory --setenv --on-active "+
				"--on-boot --on-startup --on-unit-active --on-unit-inactive --on-calendar --path-property --socket-property "+
				"--timer-property --host --machine --expand-environment --job-mode --json", "--shell --help --version")},
	"watch": {short: "bcCeghprtwx", optional: "d", valued: "nq", none: "hv",
		long: lo("--beep --color --no-color --differences --errexit --chgexit --precise --no-rerun --no-title --no-wrap --exec",
			"--interval --equexit", "--help --version")},
	"runuser": {short: "lmpfP", valued: "ugGswc", none: "hV", long: lo("--login --preserve-environment --pty",
		"--user --group --supp-group --shell --command --session-command --whitelist-environment", "--help --version")},
	"expect": {short: "dinNb", valued: "cDf", none: "v", long: lo("", "", "--help --version")},
}

// bwrapLong is bubblewrap's options (it has no short ones), by the words each takes.
var bwrapLong = func() map[string]int {
	m := lo("--unshare-all --share-net --unshare-user --unshare-user-try --unshare-ipc --unshare-pid --unshare-net --unshare-uts "+
		"--unshare-cgroup --unshare-cgroup-try --clearenv --new-session --die-with-parent --as-pid-1 --disable-userns "+
		"--assert-userns-disabled",
		"--args --argv0 --chdir --unsetenv --lock-file --sync-fd --userns --userns2 --pidns --uid --gid --hostname --remount-ro "+
			"--exec-label --file-label --dev --proc --tmpfs --mqueue --dir --seccomp --add-seccomp-fd --block-fd --userns-block-fd "+
			"--info-fd --json-status-fd --cap-add --cap-drop --perms --size --overlay-src --tmp-overlay --ro-overlay",
		"--help --version")
	for _, n := range strings.Fields("--setenv --bind --bind-try --dev-bind --dev-bind-try --ro-bind --ro-bind-try --file " +
		"--bind-data --ro-bind-data --symlink --bind-fd --ro-bind-fd --chmod") {
		m[n] = 2
	}
	m["--overlay"] = 3
	return m
}()

// An opt is an option a grammar read, with its value.
type opt struct{ name, value string }

// unknownOption is the unreadable line of an option a runner's grammar does not know.
func unknownOption(name, a string) error {
	return &Unreadable{Why: name + "'s option " + a + " is not one the reader knows, so the command " + name + " runs cannot be told"}
}

// parse reads the options of w, a runner's words: the index of the
// command, the options read, and run false when no command follows.
func (g grammar) parse(name string, w []string) (k int, seen []opt, run bool, err error) {
	k = 1
	for k < len(w) {
		a := w[k]
		if a == "--" {
			k++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		if g.oneWord {
			seen = append(seen, opt{a, ""})
			k++
			continue
		}
		if strings.HasPrefix(a, "--") {
			n, v, eq := strings.Cut(a, "=")
			c, known := g.long[n]
			if !known {
				return 0, nil, false, unknownOption(name, a)
			}
			if c < 0 {
				return 0, nil, false, nil
			}
			if !eq && c > 0 {
				v = strings.Join(w[min(k+1, len(w)):min(k+1+c, len(w))], " ")
				k += c
			}
			seen = append(seen, opt{n, v})
			k++
			continue
		}
		for i := 1; i < len(a); i++ {
			ch, n := a[i], "-"+a[i:i+1]
			switch {
			case strings.IndexByte(g.none, ch) >= 0:
				return 0, nil, false, nil
			case strings.IndexByte(g.short, ch) >= 0:
				seen = append(seen, opt{n, ""})
			case strings.IndexByte(g.optional, ch) >= 0:
				seen, i = append(seen, opt{n, a[i+1:]}), len(a)
			case strings.IndexByte(g.valued, ch) >= 0:
				v := a[i+1:]
				if v == "" {
					k++
					v = at(w, k)
				}
				seen, i = append(seen, opt{n, v}), len(a)
			default:
				return 0, nil, false, unknownOption(name, a)
			}
		}
		k++
	}
	k += g.operands
	return k, seen, k < len(w), nil
}

// has is whether seen holds one of the options names.
func has(seen []opt, names ...string) bool {
	return slices.ContainsFunc(seen, func(o opt) bool { return slices.Contains(names, o.name) })
}

// Inner is what a program runs, one level down (Unwrap): programs given as
// words, command lines, and whether the program is read as itself too.
type Inner struct {
	Words   [][]string
	Strings []string
	// Docs are texts whose substitutions it runs (print -P's prompt expansion).
	Docs []string
	Self bool
}

// runs is what the runner name, its words w, runs (Inner); ok is false for
// a program that is no runner. A runner that runs no command is itself.
func runs(name string, w []string) (in Inner, ok bool, err error) {
	itself := Inner{Self: true}
	strs := func(ss ...string) (Inner, bool, error) {
		if len(ss) == 0 {
			return itself, true, nil
		}
		return Inner{Strings: ss}, true, nil
	}
	switch name {
	case "trap", "sched", "zpty", "alias":
		return strs(later(name, w)...)
	case "emulate":
		// emulate [-LR] mode -c code runs the code in that emulation; emulate
		// itself stays read, as it may turn options on.
		for i := 1; i+1 < len(w); i++ {
			if a := w[i]; len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.Contains(a, "c") {
				return Inner{Strings: []string{w[i+1]}, Self: true}, true, nil
			}
		}
		return itself, true, nil
	case "sg":
		// sg [-] group [-c] command: the command is one string sh -c runs.
		a := w[1:]
		if at(a, 0) == "-" {
			a = a[1:]
		}
		if len(a) < 2 {
			return itself, true, nil
		}
		if a = a[1:]; a[0] == "-c" {
			a = a[1:]
		}
		return strs(strings.Join(a, " "))
	case "pee":
		// pee runs each argument as a command line.
		var ss []string
		for _, a := range w[1:] {
			if !strings.HasPrefix(a, "--") {
				ss = append(ss, a)
			}
		}
		return strs(ss...)
	case "flock":
		// flock [options] file command…, flock [options] file -c string,
		// flock [options] fd: the command runs with the lock.
		valued := set("-w", "--wait", "--timeout", "-E", "--conflict-exit-code")
		file := false
		for k := 1; k < len(w); k++ {
			a := w[k]
			switch {
			case a == "-c" || a == "--command":
				return strs(at(w, k+1))
			case strings.HasPrefix(a, "--command="):
				return strs(a[len("--command="):])
			case valued[a]:
				k++
			case strings.HasPrefix(a, "-"):
			case !file:
				file = true
			default:
				return Inner{Words: [][]string{w[k:]}}, true, nil
			}
		}
		return itself, true, nil
	case "busybox":
		// busybox runs its applet as the program it names (L477).
		if len(w) > 1 && !strings.HasPrefix(w[1], "-") {
			return Inner{Words: [][]string{w[1:]}}, true, nil
		}
		return itself, true, nil
	case "git":
		return gitRuns(w)
	case "sed":
		return sedRuns(w)
	case "print":
		// zsh's print -P expands its arguments as prompts: under PROMPT_SUBST
		// their substitutions run (T228 L8).
		var in Inner
		for k := 1; k < len(w); k++ {
			if a := w[k]; strings.HasPrefix(a, "-") && strings.Contains(a, "P") && !strings.HasPrefix(a, "--") {
				in = Inner{Docs: w[k+1:], Self: true}
				break
			}
		}
		if in.Docs == nil {
			return Inner{}, false, nil
		}
		return in, true, nil
	case "gdb":
		in, err := gdbRuns(w)
		return in, true, err
	case "perf":
		in, err := perfRuns(w)
		return in, true, err
	}
	g, known := runnerGrammars[name]
	if !known {
		return Inner{}, false, nil
	}
	if name == "runuser" {
		// runuser's `-` is --login.
		w = slices.Clone(w)
		for i := 1; i < len(w) && strings.HasPrefix(w[i], "-"); i++ {
			if w[i] == "-" {
				w[i] = "-l"
			}
		}
	}
	k, seen, run, err := g.parse(name, w)
	if err != nil {
		return Inner{}, true, err
	}
	switch name {
	case "watch":
		// watch runs its words as one string sh -c runs, unless -x execs them.
		if run && !has(seen, "-x", "--exec") {
			return strs(strings.Join(w[k:], " "))
		}
	case "runuser":
		// -c (--command, --session-command) is a string the user's shell runs;
		// with -u the words are the command, else the user and the shell's arguments.
		for _, o := range seen {
			if o.name == "-c" || o.name == "--command" || o.name == "--session-command" {
				in.Strings = append(in.Strings, o.value)
			}
		}
		if run {
			words := w[k:]
			if !has(seen, "-u", "--user") {
				words = append([]string{"sh"}, w[k+1:]...)
			}
			in.Words = [][]string{words}
		}
		if len(in.Strings) == 0 && len(in.Words) == 0 {
			return itself, true, nil
		}
		return in, true, nil
	case "expect":
		ss, err := expectRuns(seen, run)
		if err != nil {
			return Inner{}, true, err
		}
		return strs(ss...)
	}
	if !run {
		return itself, true, nil
	}
	return Inner{Words: [][]string{w[k:]}}, true, nil
}

// runnerPrograms reads a runner and what it runs (runs): the programs, and
// ok false for a program that is no runner.
func runnerPrograms(name string, w []string, self Program, sub func(string) []string, env []string, inner Opts) ([]Program, bool, error) {
	in, ok, err := runs(name, w)
	if !ok || err != nil {
		return nil, ok, err
	}
	var out []Program
	if in.Self {
		out = append(out, self)
	}
	for _, s := range in.Strings {
		ps, err := JudgeString(s, sub(name), env, inner)
		if err != nil {
			return nil, true, err
		}
		out = append(out, ps...)
	}
	for _, d := range in.Docs {
		for _, c := range documentCommands(d) {
			ps, err := CommandPrograms(c, sub(name), env, inner)
			if err != nil {
				return nil, true, err
			}
			out = append(out, ps...)
		}
	}
	for _, words := range in.Words {
		ps, err := Programs(words, sub(name), env, inner)
		if err != nil {
			return nil, true, err
		}
		out = append(out, ps...)
	}
	if len(out) == 0 {
		out = []Program{self}
	}
	return out, true, nil
}

// Unwrap is what words run one level down, for a program that runs
// another (T228 L2): zsh's precommand modifiers and repeat, a wrapper
// (sh.Wrappers: nice, timeout, stdbuf, sudo, …), an external runner
// (setsid, chrt, strace, watch, …) or a builtin that runs a string later
// (trap, sched, zpty, alias, emulate -c). ok is false for any other
// program; err is set when a runner's options cannot be read, so the
// command it runs cannot be told.
func Unwrap(words []string) (in Inner, ok bool, err error) {
	if len(words) == 0 {
		return Inner{}, false, nil
	}
	name := Base(words[0])
	if rest, ok := precommand(name, words); ok {
		return Inner{Words: [][]string{rest}}, true, nil
	}
	if in, ok, err := runs(name, words); ok {
		return in, true, err
	}
	switch name {
	case "xargs":
		// xargs runs its command with the operands it reads (XargsCommand).
		if cmd := XargsCommand(words); len(cmd) > 0 {
			return Inner{Words: [][]string{cmd}}, true, nil
		}
		return Inner{Self: true}, true, nil
	case "find":
		// find runs each -exec, -execdir, -ok and -okdir command.
		var in Inner
		for k := 1; k < len(words); k++ {
			if !FindExec[words[k]] {
				continue
			}
			e := k + 1
			for e < len(words) && words[e] != ";" && words[e] != "+" {
				e++
			}
			in.Words = append(in.Words, words[k+1:e])
			k = e
		}
		in.Self = len(in.Words) == 0
		return in, true, nil
	}
	if _, ok := Wrappers[name]; !ok {
		return Inner{}, false, nil
	}
	if name == "command" && (at(words, 1) == "-v" || at(words, 1) == "-V") {
		return Inner{Self: true}, true, nil
	}
	wr := wrapped(name, words)
	if wr.split != nil {
		return Inner{Strings: []string{*wr.split}}, true, nil
	}
	if len(wr.rest) == 0 {
		return Inner{Self: true}, true, nil
	}
	return Inner{Words: [][]string{wr.rest}}, true, nil
}

// later is the strings the builtins that run a string later run, as eval
// would run them: trap's action (trap [--] action sig…), zsh's sched
// (sched [-o] time command…) and zpty (zpty [-eb] name command…), and an
// alias's value; nil when none runs.
func later(name string, w []string) []string {
	a := w[1:]
	switch name {
	case "trap":
		if at(a, 0) == "--" {
			a = a[1:]
		}
		// trap, trap -p, trap -l, trap - sig: no action.
		if len(a) == 0 || strings.HasPrefix(a[0], "-") {
			return nil
		}
		return []string{a[0]}
	case "sched":
		if at(a, 0) == "-o" {
			a = a[1:]
		}
		// sched lists, sched -N removes an item.
		if len(a) < 2 || strings.HasPrefix(a[0], "-") {
			return nil
		}
		return []string{strings.Join(a[1:], " ")}
	case "zpty":
		for len(a) > 0 && len(a[0]) > 1 && a[0][0] == '-' {
			// -d, -w, -r, -t, -L and -n run no command.
			if strings.Trim(a[0][1:], "eb") != "" {
				return nil
			}
			a = a[1:]
		}
		if len(a) < 2 {
			return nil
		}
		return []string{strings.Join(a[1:], " ")}
	}
	var ss []string
	for _, x := range a {
		if n, v, ok := strings.Cut(x, "="); ok && n != "" && !strings.HasPrefix(x, "-") {
			ss = append(ss, v)
		}
	}
	return ss
}

// gdbReads are gdb's commands that run nothing: they read the program or set display options.
var gdbReads = set("bt", "backtrace", "where", "info", "i", "quit", "q", "continue", "c", "next", "n", "step", "s", "finish",
	"list", "l", "frame", "f", "thread", "detach", "kill", "disassemble", "show")

// gdbSets are the display settings `set` may change in a gdb command.
var gdbSets = set("pagination", "confirm", "width", "height", "logging", "print", "style", "verbose")

// gdbRuns reads gdb (getopt_long_only: -opt or --opt): its -ex commands
// that run a shell command (shell, !, pipe), and `run` or `start` of the
// program (after --args, or its first operand). A command file (-x), or a
// command that is not one of those or a read (gdbReads), may run
// anything: unreadable.
func gdbRuns(w []string) (Inner, error) {
	valued := set("ex", "eval-command", "iex", "init-eval-command", "p", "pid", "c", "core", "s", "symbols", "e", "exec", "se", "cd",
		"d", "directory", "data-directory", "D", "tty", "t", "b", "l", "interpreter", "i")
	flags := set("batch", "batch-silent", "q", "quiet", "silent", "nx", "n", "nh", "readnow", "readnever", "write", "tui",
		"return-child-result", "nw", "nowindows", "statistics", "w", "windows", "version", "help", "configuration")
	var cmds, program []string
	unreadable := func(why string) (Inner, error) {
		return Inner{}, &Unreadable{Why: "gdb " + why + ", which may run any command"}
	}
	for k := 1; k < len(w); k++ {
		a := w[k]
		if !strings.HasPrefix(a, "-") || a == "-" {
			if program == nil {
				program = []string{a}
			}
			continue
		}
		n, v, eq := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case n == "args":
			program = w[k+1:]
			k = len(w)
		case n == "x" || n == "command" || n == "ix" || n == "init-command":
			return unreadable("runs a command file")
		case valued[n]:
			if !eq {
				k++
				v = at(w, k)
			}
			if n == "ex" || n == "eval-command" || n == "iex" || n == "init-eval-command" {
				cmds = append(cmds, v)
			}
		case flags[n]:
		default:
			return unreadable("has the option " + a)
		}
	}
	var in Inner
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		first, rest, _ := strings.Cut(c, " ")
		switch {
		case first == "shell":
			in.Strings = append(in.Strings, rest)
		case strings.HasPrefix(c, "!"):
			in.Strings = append(in.Strings, c[1:])
		case first == "pipe" || strings.HasPrefix(c, "|"):
			if _, sh, ok := strings.Cut(strings.TrimPrefix(c, "|"), "|"); ok {
				in.Strings = append(in.Strings, sh)
			}
		case first == "run" || first == "r" || first == "start" || first == "starti":
			if program != nil {
				if strings.TrimSpace(rest) == "" {
					in.Words = append(in.Words, program)
				} else {
					in.Strings = append(in.Strings, program[0]+" "+rest)
				}
			}
		case gdbReads[first] || first == "set" && gdbSets[strings.Fields(rest + " x")[0]]:
		default:
			return unreadable("runs the command `" + c + "`")
		}
	}
	if len(in.Strings) == 0 && len(in.Words) == 0 {
		in.Self = true
	}
	return in, nil
}

// perfReads are perf's subcommands that run no command.
var perfReads = set("report", "list", "annotate", "diff", "help", "version", "--version", "--help", "-h", "-v", "top", "evlist",
	"buildid-list", "archive", "config", "script", "probe", "test", "bench")

// perfRuns reads perf: a subcommand that runs a command (record, stat,
// trace and the like) runs the words after `--`; without `--`, its options
// are not told from the command, and a line with a word that is not an
// option is unreadable.
func perfRuns(w []string) (Inner, error) {
	if len(w) < 2 || perfReads[w[1]] {
		return Inner{Self: true}, nil
	}
	if dd := slices.Index(w, "--"); dd >= 0 {
		if dd+1 >= len(w) {
			return Inner{Self: true}, nil
		}
		return Inner{Words: [][]string{w[dd+1:]}}, nil
	}
	for _, a := range w[2:] {
		if !strings.HasPrefix(a, "-") {
			return Inner{}, &Unreadable{Why: "perf " + w[1] + "'s options are not told from the command it runs; put the command after --"}
		}
	}
	return Inner{Self: true}, nil
}

// expectSafe are the expect commands that run no program, and those that run one (spawn, exec, system).
var expectSafe = set("expect", "send", "interact", "exit", "sleep", "set", "puts", "log_user", "wait", "close", "spawn", "exec", "system")

// expectRuns reads expect -c: each statement (split at `;` and line ends)
// is a spawn, exec or system of a command, read as a shell line, or one of
// expectSafe with no substitution, brace or variable; any other, and a
// script file, may run anything: unreadable.
func expectRuns(seen []opt, run bool) ([]string, error) {
	if run || has(seen, "-f") {
		return nil, &Unreadable{Why: "expect runs a script file the reader does not read"}
	}
	var ss []string
	for _, o := range seen {
		if o.name != "-c" {
			continue
		}
		for _, st := range strings.FieldsFunc(o.value, func(r rune) bool { return r == ';' || r == '\n' }) {
			f := strings.Fields(st)
			if len(f) == 0 {
				continue
			}
			if !expectSafe[f[0]] || strings.ContainsAny(st, "[{$") {
				return nil, &Unreadable{Why: "expect runs the Tcl `" + strings.TrimSpace(st) + "`, which may run any command"}
			}
			if f[0] == "spawn" || f[0] == "exec" || f[0] == "system" {
				f = f[1:]
				for len(f) > 0 && strings.HasPrefix(f[0], "-") {
					// spawn's -open, -leaveopen and -ignore take a value.
					if f[0] == "-open" || f[0] == "-leaveopen" || f[0] == "-ignore" {
						f = f[min(1, len(f)):]
					}
					f = f[min(1, len(f)):]
				}
				ss = append(ss, strings.Join(f, " "))
			}
		}
	}
	return ss, nil
}

// gitCommandKey is a git configuration key whose value is a command git runs.
//
// From git-config(1), the keys whose value git runs (T228 L9): core.pager,
// core.editor, core.askPass, core.fsmonitor (a hook command),
// core.sshCommand, core.gitProxy, core.alternateRefsCommand, diff.external
// and a diff driver's command and textconv, a merge driver, a filter's
// clean, smudge and process, pager.<cmd>, sequence.editor, a credential
// helper, gpg.program and gpg.<format>.program, gpg's ssh
// defaultKeyCommand, uploadpack's packObjectsHook, interactive.diffFilter,
// a difftool's or mergetool's cmd and path, man.<tool>.cmd and path,
// browser.<tool>.cmd and path (web.browser, help -w), a remote's uploadpack
// and receivepack, sendemail's sendmailCmd, toCmd, ccCmd, headerCmd and
// smtpServer (a path), instaweb.httpd, and a submodule's update `!command`.
// core.hooksPath is a folder, not a command: the hooks in it are files.
var gitCommandKey = lazyre.New(`(?i)^(?:alias\..+|core\.(?:pager|editor|fsmonitor|askpass|sshcommand|gitproxy|alternaterefscommand)|` +
	`diff\.(?:external|.+\.(?:textconv|command))|merge\..+\.driver|filter\..+\.(?:clean|smudge|process)|pager\..+|sequence\.editor|` +
	`credential\.(?:.+\.)?helper|gpg\.(?:.+\.)?program|gpg\.ssh\.defaultkeycommand|uploadpack\.packobjectshook|interactive\.difffilter|` +
	`(?:merge|diff)tool\..+\.(?:cmd|path)|(?:man|browser)\..+\.(?:cmd|path)|remote\..+\.(?:uploadpack|receivepack)|` +
	`sendemail\.(?:.+\.)?(?:sendmailcmd|tocmd|cccmd|headercmd|smtpserver)|instaweb\.httpd|submodule\..+\.update)$`)

// gitRuns reads the commands git runs from its own words (T228 L5): a -c
// value of a key whose value is a command (gitCommandKey: core.pager,
// core.editor, a filter's or a driver's command, …), read as a shell line,
// one that does not parse left out; an alias given with -c, where it is
// the subcommand and no git command has its name, with the arguments
// after it (its `!` dropped; one with none is a git command line); bisect
// run's command, rebase's --exec and submodule foreach's command. `git
// config` of an alias is left to the guard's git-config question (T152).
// git itself stays read (Self). ok is false for a line with none of these,
// which runs as git alone.
func gitRuns(w []string) (Inner, bool, error) {
	in := Inner{Self: true}
	value := func(v string) {
		if _, err := Parse(v); err == nil && strings.TrimSpace(v) != "" {
			in.Strings = append(in.Strings, v)
		}
	}
	var settings []string
	k := 1
	for ; k < len(w); k++ {
		a := w[k]
		switch {
		case a == "-c":
			k++
			settings = append(settings, at(w, k))
		case strings.HasPrefix(a, "-c") && len(a) > 2:
			settings = append(settings, a[2:])
		case GitValueGlobals[a]:
			// -C, --git-dir, --config-env and the rest take the next word (T228 L10).
			k++
		case strings.HasPrefix(a, "-"):
		default:
			goto sub
		}
	}
sub:
	rest := w[min(k, len(w)):]
	for _, kv := range settings {
		strs, err := GitSetting(kv, rest)
		if err != nil {
			return Inner{}, true, err
		}
		for _, v := range strs {
			value(v)
		}
	}
	// T228 L8: the options whose value git runs as a command, and ext:: remotes.
	for i := 1; i < len(rest); i++ {
		a := rest[i]
		n, v, eq := strings.Cut(a, "=")
		sub := at(rest, 0)
		switch {
		case strings.HasPrefix(a, "ext::"):
			in.Words = append(in.Words, extWords(a[5:]))
		case gitCommandOptions[n] && eq:
			value(v)
		case gitCommandOptions[n] && i+1 < len(rest):
			i++
			value(rest[i])
		case n == "--smtp-server":
			if !eq {
				i++
				v = at(rest, i)
			}
			if strings.Contains(v, "/") {
				in.Words = append(in.Words, []string{v})
			}
		case sub == "difftool" && a == "-x" || sub == "clone" && a == "-u" || sub == "instaweb" && a == "-d":
			i++
			value(at(rest, i))
		case sub == "grep" && strings.HasPrefix(a, "-O") && len(a) > 2:
			value(a[2:])
		}
	}
	switch at(rest, 0) {
	case "bisect":
		if at(rest, 1) == "run" && len(rest) > 2 {
			in.Words = append(in.Words, rest[2:])
		}
	case "rebase":
		for i, a := range rest {
			switch {
			case a == "-x" || a == "--exec":
				value(at(rest, i+1))
			case strings.HasPrefix(a, "--exec="):
				value(a[len("--exec="):])
			}
		}
	case "submodule":
		if i := slices.Index(rest, "foreach"); i > 0 {
			args := rest[i+1:]
			for len(args) > 0 && strings.HasPrefix(args[0], "-") {
				args = args[1:]
			}
			if len(args) > 0 {
				value(strings.Join(args, " "))
			}
		}
	}
	if len(in.Strings) == 0 && len(in.Words) == 0 {
		return Inner{}, false, nil
	}
	return in, true, nil
}

// GitSetting is the command lines a git configuration setting kv
// (key=value, from -c, --config-env or the environment) runs, for a git
// command line whose subcommand and arguments are rest: none for a key
// gitCommandKey does not name; an alias only where it is the subcommand and
// no git command has its name, with the arguments after it (its `!`
// dropped; one with none is a git command line); a credential helper's
// `!` dropped; for an ssh command, what ssh runs on this machine
// (SSHCommands, whose error makes the line unreadable). A value that does
// not parse runs nothing the reader can tell.
func GitSetting(kv string, rest []string) ([]string, error) {
	key, v, ok := strings.Cut(kv, "=")
	name, isAlias := strings.CutPrefix(strings.ToLower(key), "alias.")
	var out []string
	add := func(s string) {
		if _, err := Parse(s); err == nil && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	switch {
	case !ok || !gitCommandKey.MatchString(key):
	case isAlias:
		if name != at(rest, 0) || gitCommands[name] {
			return nil, nil
		}
		args := strings.Join(rest[min(1, len(rest)):], " ")
		if sc, shell := strings.CutPrefix(v, "!"); shell {
			add(strings.TrimSpace(sc + " " + args))
		} else {
			add(strings.TrimSpace("git " + v + " " + args))
		}
	default:
		v = strings.TrimPrefix(v, "!")
		if f, err := Parse(v); err == nil && len(f) == 1 && sshProgram(at(f[0].Words, 0)) {
			vals, err := SSHCommands(f[0].Words)
			if err != nil {
				return nil, err
			}
			for _, o := range vals {
				add(o)
			}
			return out, nil
		}
		add(v)
	}
	return out, nil
}

// gitCommandOptions are git's options whose value it runs as a command:
// difftool's --extcmd, grep's --open-files-in-pager, the transports'
// --upload-pack and --receive-pack, archive's (and push's) --exec,
// daemon's --access-hook, instaweb's --httpd and send-email's
// --sendmail-cmd (T228 L8).
var gitCommandOptions = set("--extcmd", "--open-files-in-pager", "--upload-pack", "--receive-pack", "--exec", "--access-hook",
	"--httpd", "--sendmail-cmd")

// extWords is the words an ext:: remote runs: its text split at spaces,
// `% ` a space inside a word and `%%` a percent sign (git-remote-ext).
func extWords(s string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '%' && i+1 < len(s):
			i++
			b.WriteByte(s[i])
		case s[i] == ' ':
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(s[i])
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// gitCommands are git's own commands, which an alias of the same name never replaces.
var gitCommands = set("add", "am", "annotate", "apply", "archive", "bisect", "blame", "branch", "bundle", "cat-file", "checkout",
	"cherry", "cherry-pick", "clean", "clone", "commit", "config", "count-objects", "describe", "diff", "difftool", "fetch",
	"format-patch", "fsck", "gc", "grep", "help", "init", "log", "ls-files", "ls-remote", "ls-tree", "merge", "mergetool", "mv",
	"notes", "pull", "push", "range-diff", "rebase", "reflog", "remote", "repack", "replace", "reset", "restore", "revert",
	"rev-list", "rev-parse", "rm", "shortlog", "show", "show-ref", "sparse-checkout", "stash", "status", "submodule", "switch",
	"tag", "worktree", "version")

// sedRuns reads the commands sed runs (T228 L8): GNU sed's s command with
// the e flag runs its pattern space after the substitution, read as the
// replacement (the text a whole-line match makes it), and the e command
// runs its text; `e` with none runs the pattern space, input the reader
// does not see: unreadable. The script is each -e's (--expression) value,
// else the first operand; a script file (-f) is not read. sed itself stays
// read (its -i writes). ok is false for a script that runs none.
func sedRuns(w []string) (Inner, bool, error) {
	var scripts []string
	first := true
	for k := 1; k < len(w); k++ {
		a := w[k]
		switch {
		case a == "-e" || a == "--expression":
			k++
			scripts, first = append(scripts, at(w, k)), false
		case strings.HasPrefix(a, "--expression="):
			scripts, first = append(scripts, a[len("--expression="):]), false
		case len(a) > 2 && a[0] == '-' && a[1] != '-' && strings.Contains(a, "e"):
			// A cluster ending in e takes the script after it (-ne 'script').
			if i := strings.IndexByte(a, 'e'); i == len(a)-1 {
				k++
				scripts, first = append(scripts, at(w, k)), false
			} else {
				scripts, first = append(scripts, a[i+1:]), false
			}
		case a == "-f" || a == "--file" || a == "-l" || a == "--line-length":
			k++
			first = first && a[1] != 'f' && a != "--file"
		case strings.HasPrefix(a, "-"):
		case first:
			scripts, first = append(scripts, a), false
		}
	}
	in := Inner{Self: true}
	for _, sc := range scripts {
		cmds, err := sedCommands(sc)
		if err != nil {
			return Inner{}, true, err
		}
		in.Strings = append(in.Strings, cmds...)
	}
	if len(in.Strings) == 0 {
		return Inner{}, false, nil
	}
	return in, true, nil
}

// sedCommands is the commands a sed script runs (sedRuns).
func sedCommands(sc string) ([]string, error) {
	var out []string
	// field reads the text from i up to an unescaped delim: the text and the index after the delimiter.
	field := func(i int, delim byte) (string, int) {
		var b strings.Builder
		for ; i < len(sc); i++ {
			switch {
			case sc[i] == '\\' && i+1 < len(sc):
				if sc[i+1] != delim {
					b.WriteByte('\\')
				}
				i++
				b.WriteByte(sc[i])
			case sc[i] == delim:
				return b.String(), i + 1
			default:
				b.WriteByte(sc[i])
			}
		}
		return b.String(), i
	}
	for i := 0; i < len(sc); {
		c := sc[i]
		switch {
		case c == ' ' || c == '\t' || c == ';' || c == '\n' || c == '{' || c == '}' || c == '!' || c == ',' ||
			c == '$' || c >= '0' && c <= '9' || c == '~' || c == '+':
			i++
		case c == '/':
			_, i = field(i+1, '/')
		case c == '\\' && i+1 < len(sc):
			_, i = field(i+2, sc[i+1])
		case c == 's' && i+1 < len(sc):
			d := sc[i+1]
			_, j := field(i+2, d)
			repl, j := field(j, d)
			k := j
			for k < len(sc) && strings.IndexByte(";\n}", sc[k]) < 0 {
				k++
			}
			// The flags, up to a w flag's file name.
			flags, _, _ := strings.Cut(sc[j:k], "w")
			if strings.ContainsRune(flags, 'e') {
				out = append(out, repl)
			}
			i = k
		case c == 'y' && i+1 < len(sc):
			d := sc[i+1]
			_, j := field(i+2, d)
			_, i = field(j, d)
		case c == 'e':
			end := strings.IndexByte(sc[i:], '\n')
			if end < 0 {
				end = len(sc) - i
			}
			cmd := strings.TrimSpace(sc[i+1 : i+end])
			if cmd == "" {
				return nil, &Unreadable{Why: "sed's e command runs each line of its input, which the reader does not see"}
			}
			out = append(out, cmd)
			i += end
		case strings.IndexByte("aic", c) >= 0 || strings.IndexByte("rRwWbtTv:", c) >= 0:
			// Text or a name to the end of the line.
			end := strings.IndexByte(sc[i:], '\n')
			if end < 0 {
				return out, nil
			}
			i += end
		default:
			i++
		}
	}
	return out, nil
}
