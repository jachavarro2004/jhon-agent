package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T167 (L1437, L1438): shell.keys.tasks is shell.keys.agents, an old
// file's value read under the new key and moved there by migrate.
const tasksWhy = "the key opens /jhon:agents, so it is shell.keys.agents now"

func TestTheTasksKeyMovesToAgentsEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[shell.keys]\ntasks = \"ctrl+b\" # mine\npanel = \"alt+j\"\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with shell.keys.tasks must still load: %d\n%s", r.Code, r.Out())
	}
	if line := `shell.keys.tasks = "ctrl+b" reads as shell.keys.agents = "ctrl+b": ` + tasksWhy; strings.Count(r.Stderr, line) != 1 {
		t.Errorf("list does not say once %q:\n%s", line, r.Stderr)
	}
	if row, _ := find(list(t, h, h.dir), "shell.keys.agents"); row.Value != "ctrl+b" || row.Source != "file" {
		t.Errorf("shell.keys.agents = %+v, want ctrl+b from the file", row)
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	for _, args := range [][]string{{"get", "shell.keys.tasks"}, {"set", "shell.keys.tasks", "alt+b"}} {
		if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, "shell.keys.tasks: removed in v0.4: "+tasksWhy) {
			t.Errorf("%s: %d %q, want 2 and removed in v0.4", strings.Join(args, " "), r.Code, r.Out())
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("a refused set wrote the file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	if now, want := support.Read(t, h.file()), "schema = 1\n\n[shell.keys]\npanel = \"alt+j\"\nagents = \"ctrl+b\"\n"; now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
	if r := h.jhon(t, "list"); strings.Contains(r.Stderr, "tasks") {
		t.Errorf("list after migrate still warns: %q", r.Stderr)
	}
}

// T169 (L1437, L1438): the command palette and the panel sheet are gone, and
// shell.keys.commands and shell.keys.rail with them, retired as the
// notification keys were: list warns once and leaves the file, get and set
// exit 2 saying removed in v0.4, migrate drops them with a backup.
func TestThePaletteAndSheetKeysAreRetiredEndToEnd(t *testing.T) {
	h := newHome(t, "JHON_SHELL_RAIL_KEY=ctrl+r")
	old := "schema = 1\n\n[shell.keys]\nrail = \"alt+r\"\ncommands = \"alt+k\" # mine\n\n[integrations]\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	why := map[string]string{
		"shell.keys.rail":     "the panel sheet (/jhon:panel) was removed from the shell",
		"shell.keys.commands": "the command palette (/jhon:commands) was removed from the shell",
	}
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with the old keys must still load: %d\n%s", r.Code, r.Out())
	}
	for _, line := range []string{`shell.keys.rail = "alt+r" is no longer read: ` + why["shell.keys.rail"], `shell.keys.commands = "alt+k" is no longer read: ` + why["shell.keys.commands"]} {
		if n := strings.Count(r.Stderr, line); n != 1 {
			t.Errorf("list warns %d times, want once: %q\n%s", n, line, r.Stderr)
		}
	}
	if strings.Contains(r.Stdout, "shell.keys.rail") || strings.Contains(r.Stdout, "shell.keys.commands") || strings.Contains(r.Stdout, "JHON_SHELL_RAIL_KEY") {
		t.Errorf("list shows a retired key as a setting:\n%s", r.Stdout)
	}
	for key, w := range why {
		for _, args := range [][]string{{"get", key}, {"set", key, "alt+x"}} {
			if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, key+": removed in v0.4: "+w) {
				t.Errorf("%s: %d %q, want 2 and removed in v0.4", strings.Join(args, " "), r.Code, r.Out())
			}
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list or a refused set wrote the file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	if now, want := support.Read(t, h.file()), "schema = 1\n\n[integrations]\ntelemetry = false\n"; now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
}

// T174 (L1462, L1469): the changes view, the usage view and the todo card are
// gone, and shell.keys.changes, shell.keys.usage and shell.keys.todos with
// them, retired as the palette keys were: list warns once and leaves the
// file, get and set exit 2 saying removed in v0.4, migrate drops them with a
// backup.
func TestTheChangesUsageAndTodosKeysAreRetiredEndToEnd(t *testing.T) {
	h := newHome(t, "JHON_SHELL_USAGE_KEY=ctrl+u")
	old := "schema = 1\n\n[shell.keys]\nchanges = \"alt+g\"\nusage = \"alt+y\" # mine\ntodos = \"off\"\n\n[integrations]\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	why := map[string]string{
		"shell.keys.changes": "the changes view (/jhon:changes) was removed from the shell",
		"shell.keys.usage":   "the usage view (/jhon:usage) was removed from the shell; the dashboard has the stats",
		"shell.keys.todos":   "the todo card and its keys were removed; the agent's list shows in the right pane while no LDD change is active",
	}
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with the old keys must still load: %d\n%s", r.Code, r.Out())
	}
	for _, line := range []string{
		`shell.keys.changes = "alt+g" is no longer read: ` + why["shell.keys.changes"],
		`shell.keys.usage = "alt+y" is no longer read: ` + why["shell.keys.usage"],
		`shell.keys.todos = "off" is no longer read: ` + why["shell.keys.todos"],
	} {
		if n := strings.Count(r.Stderr, line); n != 1 {
			t.Errorf("list warns %d times, want once: %q\n%s", n, line, r.Stderr)
		}
	}
	for _, gone := range []string{"shell.keys.changes", "shell.keys.usage", "shell.keys.todos", "JHON_SHELL_USAGE_KEY"} {
		if strings.Contains(r.Stdout, gone) {
			t.Errorf("list shows %s as a setting:\n%s", gone, r.Stdout)
		}
	}
	for key, w := range why {
		for _, args := range [][]string{{"get", key}, {"set", key, "alt+x"}} {
			if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, key+": removed in v0.4: "+w) {
				t.Errorf("%s: %d %q, want 2 and removed in v0.4", strings.Join(args, " "), r.Code, r.Out())
			}
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list or a refused set wrote the file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	if now, want := support.Read(t, h.file()), "schema = 1\n\n[integrations]\ntelemetry = false\n"; now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
}
