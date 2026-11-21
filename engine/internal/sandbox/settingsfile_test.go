package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// SettingsFile walks the path as the kernel does: an entry on the way that a
// sandboxed command may change, also reached through a link, stops the run;
// a `..` names a folder's parent, which no command inside it can change, and
// a missing entry outside the writable folders cannot appear (S9).
func TestSettingsFileWalksThePathAsTheKernelDoes(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, open, safe := filepath.Join(base, "home"), filepath.Join(base, "open"), filepath.Join(base, "safe")
	for _, d := range []string{home, open, safe, filepath.Join(open, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(safe, "settings.toml"), []byte("schema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		filepath.Join(safe, "loop"):     "loop",
		filepath.Join(safe, "via-open"): "../open/out",
		filepath.Join(open, "out"):      "../safe",
		filepath.Join(safe, "up"):       "../open/sub",
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Home: home, AllowWrite: []string{open}, Env: map[string]string{}, Stat: Disk}
	for _, c := range []struct {
		name, file string
		stops      bool
	}{
		{"a file outside", filepath.Join(safe, "settings.toml"), false},
		{"a missing file outside", filepath.Join(safe, "none.toml"), false},
		{"a missing folder outside", filepath.Join(safe, "none", "settings.toml"), false},
		{"a `..` out of the writable folder", open + "/../safe/settings.toml", false},
		{"a `..` after a link into the writable folder", safe + "/up/../settings.toml", true},
		{"a missing file in the writable folder", filepath.Join(open, "settings.toml"), true},
		{"a link in the writable folder to a file outside", filepath.Join(open, "out", "settings.toml"), true},
		{"a link outside whose target passes through a link in the writable folder", filepath.Join(safe, "via-open", "settings.toml"), true},
		{"a link loop", filepath.Join(safe, "loop", "settings.toml"), true},
	} {
		p := SettingsFile(in, c.file)
		if (p != nil) != c.stops {
			t.Errorf("%s (%s): problem %v, want one: %v", c.name, c.file, p, c.stops)
		}
	}
	if p := SettingsFile(in, ""); p != nil {
		t.Errorf("no JHON_SETTINGS: problem %v, want none", p)
	}
}

// A relative JHON_SETTINGS, or one through /proc, names another file in
// every folder a later jhon call starts in: it stops the run (S9, T4 verify
// mwqipnvu-9-wx2c A1).
func TestSettingsFileStopsAPathThatDependsOnTheProcess(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, open := filepath.Join(base, "home"), filepath.Join(base, "open")
	for _, d := range []string{home, open} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(home)
	in := Input{Home: home, AllowWrite: []string{open}, Env: map[string]string{}, Stat: Disk}
	for _, f := range []string{"settings.toml", "./settings.toml", "~/settings.toml", "/proc/self/cwd/settings.toml", "/proc/1/root/etc/x.toml"} {
		if SettingsFile(in, f) == nil {
			t.Errorf("%s from %s: no problem, want one", f, home)
		}
	}
	if p := SettingsFile(in, filepath.Join(home, "settings.toml")); p != nil {
		t.Errorf("an absolute file in a read-only folder: %v, want none", p)
	}
}
