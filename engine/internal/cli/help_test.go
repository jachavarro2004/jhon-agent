package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// `jhon audit --help` says where the audit result cache lives and when it
// audits again (S28; contracts/efficiency.md points here).
func TestAuditHelpExplainsTheCache(t *testing.T) {
	// capture drains the pipe while Run writes: the help is larger than a
	// one-page pipe, which Linux hands out past pipe-user-pages-soft, and a
	// read after Run hung the release's go test there (T27).
	code, b := capture(t, []string{"audit", "--help"})
	if code != 0 {
		t.Fatalf("jhon audit --help = %d", code)
	}
	want := "                           dashboard component; --json and --view are cached in\n" +
		"                           the user cache dir (jhon/audit), keyed by the session\n" +
		"                           and every input the audit reads: a stale input means\n" +
		"                           a new audit\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("the audit cache is not explained:\n%s", b)
	}
}

// TestUserCommandHelp (T10i): each command a user runs prints its own help
// for --help or -h, exit 0: its name and purpose, then its usage rows as the
// general help writes them; the general help stays for jhon --help and an
// unknown command.
func TestUserCommandHelp(t *testing.T) {
	for _, cmd := range []string{"setup", "start", "update", "doctor", "status", "version", "settings", "profile", "sandbox"} {
		for _, flag := range []string{"--help", "-h"} {
			code, out := capture(t, []string{cmd, flag})
			if code != 0 {
				t.Errorf("jhon %s %s = %d", cmd, flag, code)
			}
			if !strings.HasPrefix(out, "jhon "+cmd+" — ") || !strings.Contains(out, "\nusage\n  jhon "+cmd) {
				t.Errorf("jhon %s %s is not its own help:\n%s", cmd, flag, out)
			}
			if strings.Contains(out, "jhon sync ") || strings.Contains(out, "agent framework") {
				t.Errorf("jhon %s %s printed the general help:\n%s", cmd, flag, out)
			}
		}
	}
	_, update := capture(t, []string{"update", "--help"})
	for _, want := range []string{"jhon update [--check] [--json]", "jhon update --rollback"} {
		if !strings.Contains(update, want) {
			t.Errorf("jhon update --help lacks %q:\n%s", want, update)
		}
	}
	for _, args := range [][]string{{"--help"}, {"no-such-command", "-h"}, {"sync", "--help"}} {
		_, out := capture(t, args)
		if !strings.Contains(out, "\nmaintainer (needs the source checkout)\n") {
			t.Errorf("jhon %v is not the general help:\n%s", args, out)
		}
	}
}

// TestGeneralHelpPutsTheUserFirst (T10i): setup, start, update and doctor
// come before the maintainer's commands, which sit under their own heading.
func TestGeneralHelpPutsTheUserFirst(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	heading := strings.Index(out, "\nmaintainer (needs the source checkout)\n")
	if heading < 0 {
		t.Fatalf("no maintainer heading:\n%s", out)
	}
	for _, user := range []string{"\n  jhon setup ", "\n  jhon start ", "\n  jhon update ", "\n  jhon doctor "} {
		if i := strings.Index(out, user); i < 0 || i > heading {
			t.Errorf("%q is not before the maintainer heading", user)
		}
	}
	for _, m := range []string{"\n  jhon sync ", "\n  jhon publish ", "\n  jhon release pack ", "\n  jhon test ", "\n  jhon validate ", "\n  jhon check "} {
		if i := strings.Index(out, m); i < heading {
			t.Errorf("%q is not under the maintainer heading", m)
		}
	}
}

// TestGeneralHelpByRole (T10j, N1): a user, who has only the release, gets
// a user-facing first line and no source line; the maintainer's checkout
// keeps both.
func TestGeneralHelpByRole(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	// The install record names the source (T249): the home's own state
	// folder, never the caller's XDG_STATE_HOME.
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("JHON_SOURCE", "")
	t.Setenv("JHON_ROOT", root)
	_, user := capture(t, []string{"--help"})
	if !strings.HasPrefix(user, "jhon — the Jhon agent framework: set it up, start a session, keep it updated\n") {
		t.Errorf("a user's first line:\n%s", user)
	}
	if strings.Contains(user, "maintenance for the Jhon") || strings.Contains(user, "\n  source:") {
		t.Errorf("a user's help names the maintainer's source:\n%s", user)
	}
	if !strings.Contains(user, "\n  runtime: ") {
		t.Errorf("a user's help lost the runtime line:\n%s", user)
	}
	t.Setenv("JHON_ROOT", "")
	t.Setenv("JHON_SOURCE", repoRoot(t))
	_, maint := capture(t, []string{"--help"})
	if !strings.HasPrefix(maint, "jhon — maintenance for the Jhon agent framework\n") || !strings.Contains(maint, "\n  source:  ") {
		t.Errorf("the maintainer's help changed:\n%s", maint)
	}
}

// repoRoot is this checkout: engine/internal/cli's grandparent's parent.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// `jhon settings --help` names the keys migrate drops or moves: T167 moved
// keys.tasks to keys.agents; T169 retired keys.rail and keys.commands
// (L1438); T174 retired keys.changes (L1462); T174-S5b brought keys.usage
// and keys.todos back (L1495), so migrate no longer names them.
func TestSettingsHelpNamesTheRetiredKeys(t *testing.T) {
	_, out := capture(t, []string{"settings", "--help"})
	at := strings.Index(out, "jhon settings migrate")
	if at < 0 {
		t.Fatalf("no migrate entry:\n%s", out)
	}
	entry := out[at:]
	if end := strings.Index(entry, "jhon profile"); end > 0 {
		entry = entry[:end]
	}
	for _, key := range []string{"keys.tasks", "keys.rail", "keys.commands", "keys.changes"} {
		if !strings.Contains(entry, key) {
			t.Errorf("jhon settings --help does not name %s among the dropped keys:\n%s", key, entry)
		}
	}
	for _, kept := range []string{"keys.usage", "keys.todos"} {
		if strings.Contains(entry, kept) {
			t.Errorf("jhon settings --help names %s among the dropped keys, a setting again:\n%s", kept, entry)
		}
	}
}

// TestHelpSaysWhatTheDashboardIs (T15.13): the dashboard lists every page key
// it answers (g u and g m too), and it is not an opt-in integration any more
// (integrations.dashboard is retired; /jhon:dashboard opens it).
func TestHelpSaysWhatTheDashboardIs(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	if !strings.Contains(out, "Esc, g h/s/c/u/m/t, ctrl+k to find, ? for all; Ctrl-C") {
		t.Errorf("the dashboard's keys miss g u or g m:\n%s", out)
	}
	if strings.Contains(out, "dashboard and telemetry are opt-in") {
		t.Errorf("the dashboard is still called an opt-in integration:\n%s", out)
	}
	if !strings.Contains(out, "telemetry is opt-in (off by default)") || strings.Contains(out, "herdr") {
		t.Errorf("the settings help no longer names the opt-in integrations:\n%s", out)
	}
}

// TestSettingsHelpNamesCustomize (T15.14, L174): the shell's overlay for the
// shell.* keys is /jhon:customize; /jhon:settings is gone (L195).
func TestSettingsHelpNamesCustomize(t *testing.T) {
	_, out := capture(t, []string{"settings", "--help"})
	if !strings.Contains(out, "/jhon:customize the shell.* keys") || strings.Contains(out, "/jhon:settings") {
		t.Errorf("jhon settings --help does not name /jhon:customize for the shell.* keys:\n%s", out)
	}
}

// TestHelpSaysADashboardFromAnOlderReleaseIsRestarted (T15.16, L182): the
// dashboard, sync and update rows say what happens to a running dashboard.
func TestHelpSaysADashboardFromAnOlderReleaseIsRestarted(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	for _, want := range []string{
		"                           stops it. On a port held by a dashboard proven\n" +
			"                           Jhon's, one from another release is replaced, one\n" +
			"                           from this release left to answer; anything else\n" +
			"                           is left running and said, exit 1\n",
		"                           as is. A running dashboard proven Jhon's from\n" +
			"                           another release is restarted from the new one on\n" +
			"                           its port\n",
		"  jhon update --rollback   re-activate the previous release kept on disk; after\n" +
			"                           an update or a rollback, a running dashboard proven\n" +
			"                           Jhon's from another release is restarted on its port\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the help misses:\n%s", want)
		}
	}
}

// TestHelpNamesHowEachRuntimeRunsAnOwnerCommand (T15.20, L230): pi has no
// `!`, so grant and mode say how the owner runs them in each runtime.
func TestHelpNamesHowEachRuntimeRunsAnOwnerCommand(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	for _, want := range []string{
		"                           it yourself (with `!` in Claude Code, in your own\n" +
			"                           terminal in pi): the safety guard refuses it from\n" +
			"                           an agent\n",
		"                           yours alone: run it with `!` in Claude Code, or in\n" +
			"                           your own terminal in pi\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the help misses:\n%s", want)
		}
	}
}

// `jhon --help` lists `jhon review run`, Claude Code's native Prism review (T118d).
func TestHelpListsReviewRun(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	for _, want := range []string{"  jhon review run [--base <ref>] [--change <name>] [--timeout <seconds>]\n", "each facet prompt through claude -p", "names the prism-* facet agents as the fallback", "SIGINT or SIGTERM stops"} {
		if !strings.Contains(out, want) {
			t.Errorf("jhon --help lacks %q", want)
		}
	}
}

// T145 (L1174 E2): `jhon executions --help` says once that a unit's run (a
// bare --affected) is the default for intermediate passes and the slice base
// is for the RED record and the slice's close only; and how a wide run and a
// test that names another project's file are read (E1, E3).
func TestExecutionsHelpNamesTheInnerLoop(t *testing.T) {
	_, out := capture(t, []string{"executions", "--help"})
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"with no base it is a unit's run, the default for every intermediate pass: what changed since the change's last passing run, else since its base;",
		"so the slice base (--affected <base-ref>, or a full run) is for the RED record and the slice's closing run only;",
		"those whose tests name a changed file's whole path or import path, or a changed document,",
		"a wide one runs only the test modules the change touched, those its record holds narrowed by it;",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("jhon executions --help lacks %q", want)
		}
		if strings.Count(flat, want) > 1 {
			t.Errorf("jhon executions --help says %q more than once", want)
		}
	}
}

// T174-S5b (L1495): `jhon settings --help` names the shell's shortcut keys
// with their defaults, the jobs key among them.
func TestSettingsHelpNamesTheShellKeys(t *testing.T) {
	_, out := capture(t, []string{"settings", "--help"})
	flat := strings.Join(strings.Fields(out), " ")
	want := "shell.keys.* are the shell's shortcuts, each a key or off: panel (alt+j, the LDD status), agents (alt+s), jobs (alt+o), usage (alt+u) and todos (alt+t, folds the todo list)"
	if !strings.Contains(flat, want) {
		t.Errorf("jhon settings --help does not say %q:\n%s", want, out)
	}
}

// T242 (odd/tasks/studio-integration-v1.md S5): setup, update and doctor's
// own help say that jhon-studio is installed from its signed release, the
// newest that supports this jhon, rolled back with jhon, a development
// clone left alone, and doctor's --remote looks for a newer one. Words are
// compared across the help's line breaks.
func TestHelpSaysToolsInstallFromTheirSignedReleases(t *testing.T) {
	for cmd, wants := range map[string][]string{
		"setup": {"installs jhon-studio from its newest signed release that supports this jhon", "a development clone at ~/.local/share/jhon-studio is left alone"},
		"update": {"then updates jhon-studio, when it is on, to its newest signed release that supports the jhon that runs",
			"--json lists it under tools", "it also links jhon-studio's previous version again",
			"are noted once and left alone (exit 0)", "a version folder is linked only while it is the one jhon verified",
			"and, with jhon-studio on, that jhon setup installs it there"},
		"doctor": {"which jhon-studio release is installed (or that it is a development clone)", "--remote also looks for a newer jhon-studio release that supports this jhon"},
	} {
		_, out := capture(t, []string{cmd, "--help"})
		flat := strings.Join(strings.Fields(out), " ")
		for _, want := range wants {
			if !strings.Contains(flat, want) {
				t.Errorf("jhon %s --help lacks %q:\n%s", cmd, want, out)
			}
		}
	}
}
