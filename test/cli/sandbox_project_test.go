// The sandbox's project is pinned once per session (T128c, L894): `jhon
// sandbox status` names the folder `jhon sandbox run` would take from the
// working directory, and `jhon start` exports it to the runtime as
// JHON_SANDBOX_PROJECT before it replaces itself, so every later wrap passes
// the same folder as --project whatever a command did to `.git` since.
//
// The runtimes are fakes named by JHON_CLAUDE and JHON_PI: no test starts a
// real session.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// machine is a scratch home with a git project in it, a folder beside the
// project in no repository, and the environment of a command run there.
type machine struct {
	home, proj, plain string
	env               []string
}

func newMachine(t *testing.T) machine {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := machine{home: filepath.Join(base, "home", "u")}
	m.proj, m.plain = filepath.Join(m.home, "project"), filepath.Join(m.home, "plain")
	for _, d := range []string{filepath.Join(m.proj, "src", "deep"), m.plain, filepath.Join(m.home, ".pi", "jhon")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, m.proj)
	support.Write(t, m.home, map[string]string{".pi/jhon/settings.json": "{}\n"})
	for _, kv := range support.Env() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "JHON_") || strings.HasPrefix(k, "XDG_") || k == "HOME" {
			continue
		}
		m.env = append(m.env, kv)
	}
	m.env = append(m.env, "HOME="+m.home, "JHON_TELEMETRY=off", "JHON_ROOT="+support.Root())
	return m
}

// statusProject is the project `jhon sandbox status --json` names from dir.
func (m machine) statusProject(t *testing.T, dir string, args ...string) string {
	t.Helper()
	r := support.Jhon(t, dir, m.env, append([]string{"sandbox", "status", "--json"}, args...)...)
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("status --json %v in %s: exit %d, stderr %q", args, dir, r.Code, r.Stderr)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("status --json printed %q: %v", r.Stdout, err)
	}
	p, ok := s["project"].(string)
	if !ok {
		t.Fatalf("status --json has no project string: %q", r.Stdout)
	}
	return p
}

func TestStatusNamesTheProjectRunWouldTake(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	if got := m.statusProject(t, m.proj); got != m.proj {
		t.Errorf("in the project: %q, want %q", got, m.proj)
	}
	if got := m.statusProject(t, filepath.Join(m.proj, "src", "deep")); got != m.proj {
		t.Errorf("in a folder of the project: %q, want %q", got, m.proj)
	}
	if got := m.statusProject(t, m.plain); got != "" {
		t.Errorf("in no repository: %q, want none", got)
	}
	if got := m.statusProject(t, m.proj, "--project", m.plain); got != m.plain {
		t.Errorf("with --project: %q, want %q", got, m.plain)
	}
	if got := m.statusProject(t, m.plain, "--project="+m.proj); got != m.proj {
		t.Errorf("with --project= from another folder: %q, want %q", got, m.proj)
	}
}

func TestStatusSaysTheProjectOnItsLine(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.off(t)
	r := support.Jhon(t, filepath.Join(m.proj, "src"), m.env, "sandbox", "status")
	if r.Code != 0 || strings.Count(r.Stdout, "\n") != 1 || !strings.HasPrefix(r.Stdout, "sandbox off; ") || !strings.HasSuffix(r.Stdout, "; project "+m.proj+"\n") {
		t.Errorf("in the project: exit %d, %q; want one line that ends with the project", r.Code, r.Stdout)
	}
	r = support.Jhon(t, m.plain, m.env, "sandbox", "status")
	if r.Code != 0 || strings.Count(r.Stdout, "\n") != 1 || !strings.HasSuffix(r.Stdout, "; no project here (no git repository)\n") {
		t.Errorf("in no repository: exit %d, %q; want one line that says there is no project", r.Code, r.Stdout)
	}
}

// printEnv is a fake runtime at path that writes its arguments and its
// environment, each NUL-terminated, to seen.argv and seen.env.
func printEnv(t *testing.T, path, seen string) {
	t.Helper()
	script := "#!/bin/sh\nfor a; do printf '%s\\000' \"$a\"; done > '" + seen + ".argv'\nenv -0 > '" + seen + ".env'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// started is what a fake runtime saw: its arguments, and the values of
// JHON_SANDBOX_PROJECT in its environment (none when it is unset).
type started struct {
	argv, project []string
}

// start runs `jhon start <args>` in dir with the fake runtimes and extra
// variables.
func (m machine) start(t *testing.T, dir string, extra []string, args ...string) started {
	t.Helper()
	bin := t.TempDir()
	seen := filepath.Join(bin, "seen")
	printEnv(t, filepath.Join(bin, "claude"), seen)
	printEnv(t, filepath.Join(bin, "pi"), seen)
	env := append(append(slices.Clone(m.env), "JHON_CLAUDE="+filepath.Join(bin, "claude"), "JHON_PI="+filepath.Join(bin, "pi")), extra...)
	r := support.Jhon(t, dir, env, append([]string{"start"}, args...)...)
	if r.Code != 0 {
		t.Fatalf("jhon start %v: exit %d, stderr %q", args, r.Code, r.Stderr)
	}
	var s started
	s.argv = nulList(t, seen+".argv")
	for _, kv := range nulList(t, seen+".env") {
		if v, ok := strings.CutPrefix(kv, "JHON_SANDBOX_PROJECT="); ok {
			s.project = append(s.project, v)
		}
	}
	return s
}

func nulList(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the fake runtime never ran: %v", err)
	}
	if len(b) == 0 {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

func TestStartExportsTheSandboxProject(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	for _, c := range []struct {
		name string
		args []string
	}{{"claude", nil}, {"pi", []string{"pi"}}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := m.start(t, m.proj, nil, c.args...).project; !slices.Equal(got, []string{m.proj}) {
				t.Errorf("in the project: JHON_SANDBOX_PROJECT is %q, want %q once", got, m.proj)
			}
			if got := m.start(t, filepath.Join(m.proj, "src", "deep"), nil, c.args...).project; !slices.Equal(got, []string{m.proj}) {
				t.Errorf("in a folder of the project: JHON_SANDBOX_PROJECT is %q, want the folder holding .git, %q", got, m.proj)
			}
			// A value already in the environment is replaced, never trusted.
			if got := m.start(t, m.proj, []string{"JHON_SANDBOX_PROJECT=" + m.plain}, c.args...).project; !slices.Equal(got, []string{m.proj}) {
				t.Errorf("with another value in the environment: JHON_SANDBOX_PROJECT is %q, want %q once", got, m.proj)
			}
			if got := m.start(t, m.plain, nil, c.args...).project; len(got) != 0 {
				t.Errorf("in no repository: JHON_SANDBOX_PROJECT is %q, want it unset", got)
			}
			if got := m.start(t, m.plain, []string{"JHON_SANDBOX_PROJECT=" + m.proj}, c.args...).project; len(got) != 0 {
				t.Errorf("in no repository with a value in the environment: JHON_SANDBOX_PROJECT is %q, want it unset", got)
			}
		})
	}
}

// The export changes nothing of what each runtime is started with.
func TestStartKeepsEachRuntimesArguments(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	plugin := filepath.Join(support.Root(), "runtimes", "claude-code", "plugin")
	want := []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "--resume", "abc", "-n", "work"}
	if got := m.start(t, m.proj, nil, "--resume", "abc", "-n", "work").argv; !slices.Equal(got, want) {
		t.Errorf("claude's arguments are %q, want %q", got, want)
	}
	want = []string{"--continue", "-p", "hi"}
	if got := m.start(t, m.proj, nil, "pi", "--continue", "-p", "hi").argv; !slices.Equal(got, want) {
		t.Errorf("pi's arguments are %q, want %q", got, want)
	}
}
