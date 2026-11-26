package sh

import (
	"slices"
	"strings"
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
	Self    bool
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
	values, ok := Wrappers[name]
	if !ok {
		return Inner{}, false, nil
	}
	if name == "command" && (at(words, 1) == "-v" || at(words, 1) == "-V") {
		return Inner{Self: true}, true, nil
	}
	k := 1
	for k < len(words) && (strings.HasPrefix(words[k], "-") || (name == "env" && Assignment.MatchString(words[k]))) {
		a := words[k]
		if name == "env" && (a == "-S" || a == "--split-string") {
			return Inner{Strings: []string{at(words, k+1)}}, true, nil
		}
		k += 1 + values[a]
	}
	if name == "timeout" && k < len(words) {
		k++
	}
	if k >= len(words) {
		return Inner{Self: true}, true, nil
	}
	return Inner{Words: [][]string{words[k:]}}, true, nil
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
