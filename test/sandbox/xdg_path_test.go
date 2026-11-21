package sandbox

// Jhon's own files under XDG_CONFIG_HOME and XDG_STATE_HOME
// (sandbox-deploy-tools S10): with JHON_SETTINGS unset, the settings file is
// $XDG_CONFIG_HOME/jhon/settings.toml and the state folder that holds the
// grants and the modes is $XDG_STATE_HOME/jhon. Where a sandboxed command
// could create, replace or rename either, it could write
// `[sandbox] mode = "off"` or grant itself deploy, as with S9's JHON_SETTINGS:
// `jhon sandbox run` runs nothing then. A read-only bind keeps a folder only
// where the kernel refuses to move it: a mount point cannot be renamed,
// replaced or removed from inside the sandbox (EBUSY), its plain parent can.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// configRefusal is what `jhon sandbox run` prints when the settings file
// under XDG_CONFIG_HOME is in a folder its sandbox may write.
func configRefusal(file string) string {
	return "jhon sandbox: the settings file under XDG_CONFIG_HOME (" + file + ") is in a folder a sandboxed command may write," +
		" where a command could create or replace it and turn the sandbox off, so nothing was run;" +
		" fix: set XDG_CONFIG_HOME to a folder outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write," +
		" or unset it to use ~/.config/jhon/settings.toml\n"
}

// stateRefusal is what `jhon sandbox run` prints when Jhon's state folder
// under XDG_STATE_HOME is in a folder its sandbox may write.
func stateRefusal(dir string) string {
	return "jhon sandbox: Jhon's state folder under XDG_STATE_HOME (" + dir + "), which holds the grants and the modes," +
		" is in a folder a sandboxed command may write, where a command could create or replace it and grant itself" +
		" what the guards ask for, so nothing was run;" +
		" fix: set XDG_STATE_HOME to a folder outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write," +
		" or unset it to use ~/.local/state/jhon\n"
}

func TestJhonsFilesUnderXDGInAWritableFolderStopTheSandbox(t *testing.T) {
	b := newBox(t)
	bin := support.Bin(t)
	missingCfg, keptCfg := filepath.Join(b.proj, "cfg"), filepath.Join(b.proj, "kept-cfg")
	missingState, tmpState := filepath.Join(b.proj, "state"), filepath.Join(b.tmp, "state")
	support.Write(t, keptCfg, map[string]string{"jhon/settings.toml": "schema = 1\n"})
	if err := os.MkdirAll(filepath.Join(tmpState, "jhon", "grants"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, env, want, script string
		args                    []string
		// file is what must hold read afterwards ("" for: not there).
		file, read string
		// gone is what the command would have renamed.
		gone string
	}{
		{"XDG_CONFIG_HOME, its folder missing in the project", "XDG_CONFIG_HOME=" + missingCfg,
			configRefusal(filepath.Join(missingCfg, "jhon", "settings.toml")),
			`mkdir -p "$1/jhon" && printf "$2" > "$1/jhon/settings.toml"`, []string{missingCfg, modeOffText},
			filepath.Join(missingCfg, "jhon", "settings.toml"), "", ""},
		{"XDG_CONFIG_HOME, its folder in the project renamed", "XDG_CONFIG_HOME=" + keptCfg,
			configRefusal(filepath.Join(keptCfg, "jhon", "settings.toml")),
			`mv "$1" "$1.old" && mkdir -p "$1/jhon" && printf "$2" > "$1/jhon/settings.toml"`, []string{keptCfg, modeOffText},
			filepath.Join(keptCfg, "jhon", "settings.toml"), "schema = 1\n", keptCfg + ".old"},
		{"XDG_STATE_HOME in the project, a sandboxed jhon grant deploy", "XDG_STATE_HOME=" + missingState,
			stateRefusal(filepath.Join(missingState, "jhon")),
			`"$1" grant deploy`, []string{bin},
			filepath.Join(missingState, "jhon"), "", ""},
		{"XDG_STATE_HOME in $TMPDIR, its folder renamed, then jhon grant deploy", "XDG_STATE_HOME=" + tmpState,
			stateRefusal(filepath.Join(tmpState, "jhon")),
			`mv "$2" "$2.old" && "$1" grant deploy`, []string{bin, tmpState},
			"", "", tmpState + ".old"},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := append(append([]string{}, b.env...), c.env)
			r := support.Jhon(t, b.proj, env, append([]string{"sandbox", "run", "--", "sh", "-c", c.script, "sh"}, c.args...)...)
			if r.Code != 126 || r.Stdout != "" || r.Stderr != c.want {
				t.Errorf("run: exit %d, stdout %q, stderr %q; want 126, nothing, %q", r.Code, r.Stdout, r.Stderr, c.want)
			}
			if c.file != "" && c.read == "" && exists(c.file) {
				t.Errorf("%s was created", c.file)
			} else if c.read != "" {
				if got := support.Read(t, c.file); got != c.read {
					t.Errorf("%s holds %q, want %q", c.file, got, c.read)
				}
			}
			if c.gone != "" && exists(c.gone) {
				t.Errorf("%s was renamed to %s", strings.TrimSuffix(c.gone, ".old"), c.gone)
			}
			if g := support.Jhon(t, b.proj, env, "settings", "get", "sandbox.mode"); g.Code != 0 || g.Stdout != "on\n" {
				t.Errorf("settings get sandbox.mode: exit %d, stdout %q, stderr %q; want 0 and on", g.Code, g.Stdout, g.Stderr)
			}
			if g := support.Jhon(t, b.proj, env, "grant", "--list"); g.Code != 0 || g.Stdout != "no grants\n" {
				t.Errorf("grant --list: exit %d, stdout %q, stderr %q; want 0 and no grants", g.Code, g.Stdout, g.Stderr)
			}
		})
	}
}

// Where nothing a sandboxed command may write holds them, Jhon's own files
// run as before: the defaults under the home, XDG folders outside the
// writable ones, and a JHON_SETTINGS file outside them with XDG_CONFIG_HOME
// in the project (the settings are JHON_SETTINGS', so the folder there is
// no settings). The program runs, and a sandboxed `jhon grant deploy` fails
// on a read-only file system.
func TestJhonsFilesOutsideTheWritableFoldersRunAsBefore(t *testing.T) {
	b := newBox(t)
	bin := support.Bin(t)
	b.settings(t, "schema = 1\n")
	for _, d := range []string{
		filepath.Join(b.home, ".local", "state", "jhon", "grants"),
		filepath.Join(b.base, "xdg-state", "jhon", "grants"),
		filepath.Join(b.base, "xdg-config", "jhon"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.Write(t, b.base, map[string]string{"xdg-config/jhon/settings.toml": "schema = 1\n", "safe/settings.toml": "schema = 1\n"})
	for _, c := range []struct {
		name string
		env  []string
	}{
		{"the defaults under the home", nil},
		{"XDG folders outside the writable ones", []string{"XDG_CONFIG_HOME=" + filepath.Join(b.base, "xdg-config"), "XDG_STATE_HOME=" + filepath.Join(b.base, "xdg-state")}},
		{"JHON_SETTINGS outside, XDG_CONFIG_HOME in the project", []string{"JHON_SETTINGS=" + filepath.Join(b.base, "safe", "settings.toml"), "XDG_CONFIG_HOME=" + filepath.Join(b.proj, "cfg")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := append(append([]string{}, b.env...), c.env...)
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo done`)
			if r.Code != 0 || r.Stdout != "done\n" || r.Stderr != "" {
				t.Errorf("run: exit %d, stdout %q, stderr %q; want 0, done, nothing", r.Code, r.Stdout, r.Stderr)
			}
			r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", bin, "grant", "deploy")
			if r.Code == 0 || r.Code == 126 || r.Stdout != "" || !strings.Contains(r.Stderr, "read-only file system") {
				t.Errorf("a sandboxed jhon grant deploy: exit %d, stdout %q, stderr %q; want a failure on a read-only file system", r.Code, r.Stdout, r.Stderr)
			}
			if g := support.Jhon(t, b.proj, env, "grant", "--list"); g.Code != 0 || g.Stdout != "no grants\n" {
				t.Errorf("grant --list: exit %d, stdout %q, stderr %q; want 0 and no grants", g.Code, g.Stdout, g.Stderr)
			}
		})
	}
}
