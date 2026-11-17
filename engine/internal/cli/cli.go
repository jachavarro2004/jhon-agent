// Package cli routes a jhon invocation: to a command or a hook, every one
// in Go.
package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// A Command runs with its arguments and returns the exit code.
type Command func(args []string) (int, error)

// The commands, by name; the hooks are hook.Handlers. A command
// registers itself from its own file (cmd_<name>.go) with register, in an
// init function.
var commands = map[string]Command{}

func init() {
	render.Commands = names
	render.Theme = func() string {
		p, _ := palette.Current()
		return palette.PiThemeText(p)
	}
}

// names is every subcommand jhon dispatches: the commands, help, and hook.
func names() []string {
	out := []string{"help", "hook"}
	for name := range commands {
		out = append(out, name)
	}
	return out
}

// register adds a command.
func register(name string, c Command) {
	if _, dup := commands[name]; dup {
		panic("command registered twice: " + name)
	}
	commands[name] = c
}

// Run runs one jhon invocation: `hook <name> [args]` or `<command> [args]`.
func Run(args []string) (int, error) {
	if len(args) > 0 && args[0] == "hook" {
		if len(args) < 2 {
			return 2, errors.New("usage: jhon hook <name> [args]")
		}
		if h, ok := hook.Handlers[args[1]]; ok {
			return hook.Recorded(args[1], args[2:], h, hook.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, hookRecorder()), nil
		}
		return 2, errors.New("unknown hook: " + args[1])
	}
	// docket BB1: a help flag anywhere after the command prints the usage
	// and runs nothing; no command reads -h or --help as an operand.
	// render-engine is the renderer itself, whose own --help prints its usage;
	// what follows `jhon sandbox run --` is the wrapped program's (T128a).
	if len(args) > 1 && args[0] != "render-engine" && slices.ContainsFunc(ownArgs(args), func(a string) bool { return a == "-h" || a == "--help" }) {
		// A user command has its own help (T10i); the rest, the usage.
		if !commandHelp(os.Stdout, out(), args[0]) {
			help(os.Stdout, out())
		}
		return 0, nil
	}
	name := "help" // help when there is no argument, or an empty one
	if len(args) > 0 && args[0] != "" {
		name = args[0]
	}
	switch name {
	case "help", "-h", "--help":
		help(os.Stdout, out())
		return 0, nil
	case "--version":
		name = "version"
	}
	if c, ok := commands[name]; ok {
		if !slices.Contains(recordedCommands, name) {
			return guarded(c, args[1:])
		}
		// Whether to record is decided after the command runs: a first
		// sync's migration may turn telemetry on (T61, L208).
		start := time.Now()
		code, err := guarded(c, args[1:])
		if telemetryOn() {
			recordCommand(name, args[1:], code, err, time.Since(start))
		}
		return code, err
	}
	return unknownCommand(name)
}

// ownArgs are the arguments after the command that are the command's own:
// all of them, but for sandbox, whose arguments after `--` are the program's.
func ownArgs(args []string) []string {
	own := args[1:]
	if i := slices.Index(own, "--"); args[0] == "sandbox" && i >= 0 {
		return own[:i]
	}
	return own
}

// guarded runs a command; a panic on what a command cannot recover from
// (a broken vocabulary, a null package.json) fails it with exit 1 and its
// message instead of a Go stack trace.
func guarded(c Command, args []string) (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			code, err = 1, fmt.Errorf("%v", r)
		}
	}()
	return c(args)
}

// Root is the release or checkout the binary belongs to, the runtime
// root: JHON_ROOT when set, else the parent of the directory holding the
// binary as the shell finds itself (release.Here), so the CLI linked from
// ~/.local/bin answers from ~/.local/share/jhon/current.
func Root() (string, error) {
	return release.Locate().Runtime, nil
}

// telemetryOn is whether this run records events: telemetry is an opt-in
// integration (T55), on when integrations.telemetry is (JHON_TELEMETRY
// overrides it), and never in a test binary's run unless a test turns it on
// (telemetry.Off).
func telemetryOn() bool {
	return !telemetry.Off() && settings.On("integrations.telemetry")
}

// telemetryRecorder hands the hooks' events to telemetry.
type telemetryRecorder struct{}

func (telemetryRecorder) Record(name, severity string, attrs map[string]any) {
	telemetry.Record(telemetry.Event{Name: name, Severity: severity, Attributes: attrs})
}

// hookRecorder is what records the hooks' decisions: telemetry when it is
// on, nothing otherwise.
func hookRecorder() hook.Recorder {
	if telemetryOn() {
		return telemetryRecorder{}
	}
	return nil
}

// recordedCommands are the commands that act or decide, each recorded as a
// jhon.command event; the reads a status bar runs every few seconds (status,
// audit, sessions) are not.
var recordedCommands = []string{"archive", "deliver", "executions", "grant", "init", "judgment", "mutate", "profile", "publish", "release", "review", "risk", "start", "sync", "testsuite"}

// recordCommand records one command's run: its verb (the first operand that
// is not a flag), its exit code and error, its duration, and what it noted.
func recordCommand(name string, args []string, code int, err error, took time.Duration) {
	attrs := telemetry.Noted()
	attrs["jhon.command"] = name
	if i := slices.IndexFunc(args, func(a string) bool { return a != "" && a[0] != '-' }); i >= 0 {
		attrs["jhon.command.verb"] = args[i]
	}
	attrs["process.exit.code"] = code
	attrs["jhon.duration_ms"] = took.Milliseconds()
	if wd, e := os.Getwd(); e == nil {
		attrs["jhon.cwd"] = wd
	}
	severity := "INFO"
	if err != nil || code != 0 {
		severity = "WARN"
		if err != nil {
			attrs["error.type"] = err.Error()
		}
	}
	telemetry.Record(telemetry.Event{Name: "jhon.command", Severity: severity, Attributes: attrs})
}
