package sandbox

// The settings file JHON_SETTINGS names (sandbox-deploy-tools S9): where a
// sandboxed command could create, rewrite, rename or relink it, it could
// write `[sandbox] mode = "off"` and turn the sandbox off. A read-only bind
// does not keep such a file: a missing one is not bound at all, and a bound
// one moves away with its folder when the command renames the folder. So
// `jhon sandbox run` runs nothing while that file lies in a folder its
// sandbox may write, and reads one outside them as before.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// settingsRefusal is what `jhon sandbox run` prints when the settings file
// JHON_SETTINGS names is in a folder its sandbox may write.
func settingsRefusal(file string) string {
	return "jhon sandbox: the settings file JHON_SETTINGS names (" + file + ") is in a folder a sandboxed command may write," +
		" where a command could rewrite it and turn the sandbox off, so nothing was run;" +
		" fix: keep the file outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or unset JHON_SETTINGS" +
		" to use ~/.config/jhon/settings.toml\n"
}

const modeOffText = "[sandbox]\nmode = \"off\"\n"

func TestASettingsFileInAWritableFolderStopsTheSandbox(t *testing.T) {
	b := newBox(t)
	slash, err := os.MkdirTemp("/tmp", "jhon-sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(slash) })
	extra := filepath.Join(b.base, "extra")
	support.Write(t, b.base, map[string]string{
		"extra/settings.toml": "schema = 1\n\n[sandbox]\nallow_write = [\"" + extra + "\"]\n",
		"safe/settings.toml":  "schema = 1\n",
	})
	support.Write(t, b.proj, map[string]string{"cfg/settings.toml": "schema = 1\n"})
	if err := os.Symlink(filepath.Join(b.base, "safe", "settings.toml"), filepath.Join(b.proj, "link.toml")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b.proj, "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.proj, "linked"), filepath.Join(b.base, "to-project")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, file, script, read, mode string
	}{
		{"missing, in the project", filepath.Join(b.proj, "settings.toml"), `printf "$2" > "$1"`, "", "on"},
		{"missing, in $TMPDIR", filepath.Join(b.tmp, "settings.toml"), `printf "$2" > "$1"`, "", "on"},
		{"missing, in /tmp", filepath.Join(slash, "settings.toml"), `printf "$2" > "$1"`, "", "on"},
		{"in its own sandbox.allow_write folder", filepath.Join(extra, "settings.toml"),
			`mv "$1" "$1.old" && printf "$2" > "$1"`, "schema = 1\n\n[sandbox]\nallow_write = [\"" + extra + "\"]\n", "on"},
		{"bound read-only in a project folder a command renames", filepath.Join(b.proj, "cfg", "settings.toml"),
			`mv "${1%/*}" "${1%/*}.old" && mkdir "${1%/*}" && printf "$2" > "$1"`, "schema = 1\n", "on"},
		{"a link in the project to a file outside", filepath.Join(b.proj, "link.toml"),
			`rm "$1" && printf "$2" > "$1"`, "schema = 1\n", "on"},
		{"missing, through a link outside to a project folder", filepath.Join(b.base, "to-project", "settings.toml"),
			`printf "$2" > "$1"`, "", "on"},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := append(append([]string{}, b.env...), "JHON_SETTINGS="+c.file)
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", c.script, "sh", c.file, modeOffText)
			if want := settingsRefusal(c.file); r.Code != 126 || r.Stdout != "" || r.Stderr != want {
				t.Errorf("run: exit %d, stdout %q, stderr %q; want 126, nothing, %q", r.Code, r.Stdout, r.Stderr, want)
			}
			if c.read == "" && exists(c.file) {
				t.Errorf("%s was created: %q", c.file, support.Read(t, c.file))
			} else if c.read != "" {
				if got := support.Read(t, c.file); got != c.read {
					t.Errorf("%s holds %q, want %q", c.file, got, c.read)
				}
			}
			if g := support.Jhon(t, b.proj, env, "settings", "get", "sandbox.mode"); g.Code != 0 || g.Stdout != c.mode+"\n" {
				t.Errorf("settings get sandbox.mode: exit %d, stdout %q, stderr %q; want 0 and %s", g.Code, g.Stdout, g.Stderr, c.mode)
			}
		})
	}
}

// A settings file outside every folder the sandbox may write is read and
// kept read-only as before, and so is the default one.
func TestASettingsFileOutsideTheWritableFoldersIsReadAsBefore(t *testing.T) {
	b := newBox(t)
	extra := filepath.Join(b.base, "extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "schema = 1\n\n[sandbox]\nmode = \"on\"\nallow_write = [\"" + extra + "\"]\n"
	support.Write(t, b.base, map[string]string{"safe/settings.toml": text})
	named := filepath.Join(b.base, "safe", "settings.toml")
	def := b.settings(t, text)
	for _, c := range []struct{ name, file, env string }{
		{"named by JHON_SETTINGS", named, "JHON_SETTINGS=" + named},
		{"the default", def, "JHON_SETTINGS="},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := append(append([]string{}, b.env...), c.env)
			written := filepath.Join(extra, "written-"+filepath.Base(filepath.Dir(c.file))+".txt")
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1" && echo done`, "sh", written)
			if r.Code != 0 || r.Stdout != "done\n" || r.Stderr != "" {
				t.Errorf("a write to its sandbox.allow_write folder: exit %d, stdout %q, stderr %q; want 0, done, nothing", r.Code, r.Stdout, r.Stderr)
			}
			r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `printf "$2" > "$1"`, "sh", c.file, modeOffText)
			if r.Code == 0 || r.Stdout != "" || !strings.Contains(r.Stderr, "Read-only file system") {
				t.Errorf("a write to the settings: exit %d, stdout %q, stderr %q; want a failure on a read-only file system", r.Code, r.Stdout, r.Stderr)
			}
			if got := support.Read(t, c.file); got != text {
				t.Errorf("the settings now hold %q", got)
			}
			if g := support.Jhon(t, b.proj, env, "settings", "get", "sandbox.mode"); g.Code != 0 || g.Stdout != "on\n" {
				t.Errorf("settings get sandbox.mode: exit %d, stdout %q, stderr %q; want 0 and on", g.Code, g.Stdout, g.Stderr)
			}
		})
	}
}
