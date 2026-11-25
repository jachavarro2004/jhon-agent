package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// L28 ("Rechazar el allow_write que las contenga"): a sandbox.allow_write
// folder that is or holds Jhon's release, the jhon program, the source
// checkout, one of the protected paths, or a PATH folder outside the project
// stops the run, with the fix; one a command cannot use to replace them
// (a folder beside them, or one they are bound read-only directly inside)
// does not.
func TestAnAllowWriteThatHoldsJhonsOwnPathsStopsTheRun(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home", "u")
	source, share, program := filepath.Join(home, "Projects", "personal", "jhon"), filepath.Join(home, ".local", "share", "jhon"), filepath.Join(home, ".local", "bin", "jhon")
	project, shims := filepath.Join(home, "Projects", "app"), filepath.Join(home, ".local", "share", "mise", "shims")
	for _, d := range []string{filepath.Join(source, ".git"), share, filepath.Dir(program), filepath.Join(project, "node_modules", ".bin"), shims, filepath.Join(home, ".local", "share", "other")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(program, []byte("owner\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	kept := []Kept{{What: "Jhon's release folder", Path: share}, {What: "the jhon program", Path: program}, {What: "Jhon's source checkout", Path: source}}
	input := func(allow ...string) Input {
		return Input{
			Home: home, Env: map[string]string{}, Stat: Disk, Lstat: DiskLstat,
			Roots:      []string{project},
			Path:       []string{filepath.Join(project, "node_modules", ".bin"), shims, "/usr/bin"},
			Protected:  []string{source},
			AllowWrite: allow,
		}
	}
	const fix = "name a concrete subfolder in sandbox.allow_write, such as ~/.local/share/<app>, not a folder that holds Jhon's own files or a PATH folder"
	for _, c := range []struct {
		allow  []string
		reason string
	}{
		{[]string{filepath.Join(home, "Projects")}, "sandbox.allow_write folder " + filepath.Join(home, "Projects") + " holds Jhon's source checkout (" + source + "), which a sandboxed command could then replace by renaming a folder around it"},
		{[]string{filepath.Join(home, ".local")}, "sandbox.allow_write folder " + filepath.Join(home, ".local") + " holds Jhon's release folder (" + share + "), which a sandboxed command could then replace by renaming a folder around it"},
		{[]string{filepath.Join(home, ".local", "bin")}, "sandbox.allow_write folder " + filepath.Join(home, ".local", "bin") + " holds the jhon program (" + program + "), which a sandboxed command could then replace by renaming a folder around it"},
		{[]string{filepath.Join(home, ".local", "share", "mise")}, "sandbox.allow_write folder " + filepath.Join(home, ".local", "share", "mise") + " holds the PATH folder " + shims + ", where a sandboxed command could put a program run later outside the sandbox"},
		{[]string{shims}, "sandbox.allow_write folder " + shims + " is the PATH folder " + shims + ", where a sandboxed command could put a program run later outside the sandbox"},
		{[]string{filepath.Join(home, ".local", "share", "other")}, ""},
		{[]string{project}, ""},
		// The checkout bound read-only directly inside the allowed folder is
		// a mount point no command can move (S10).
		{[]string{filepath.Join(home, "Projects", "personal")}, ""},
		{[]string{filepath.Join(home, ".local", "share")}, "sandbox.allow_write folder " + filepath.Join(home, ".local", "share") + " holds the PATH folder " + shims + ", where a sandboxed command could put a program run later outside the sandbox"},
	} {
		p := KeptProblem(input(c.allow...), kept)
		switch {
		case c.reason == "" && p != nil:
			t.Errorf("allow_write %v: %+v; want no problem", c.allow, p)
		case c.reason != "" && (p == nil || p.Reason != c.reason || p.Fix != fix):
			t.Errorf("allow_write %v: %+v; want %q with the fix", c.allow, p, c.reason)
		}
	}
	// The release folder bound read-only directly inside the allowed folder
	// is a mount point no command can move (S10): kept.
	in := input(filepath.Join(home, ".local", "share"))
	in.Path = []string{"/usr/bin"}
	if p := KeptProblem(in, kept); p != nil {
		t.Errorf("the release folder bound read-only directly inside the allowed folder is refused: %+v", p)
	}
}
