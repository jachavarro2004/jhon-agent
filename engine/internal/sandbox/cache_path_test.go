package sandbox

import (
	"slices"
	"strings"
	"testing"
)

// L29 ("Solo cachés y stores del home"): a default cache or store under the
// home that holds a PATH folder outside the project, at any depth, is not
// bound writable: a command could rename a folder around the PATH folder
// and put its own program there. Status names it with the fix. /tmp,
// $TMPDIR, a cache a variable names outside the home and a PATH folder in
// the project change nothing; what a command may write under any PATH
// (writable) still counts the cache.
func TestAHomeCacheThatHoldsAPATHFolderIsNotBoundWritable(t *testing.T) {
	d := with(machine, disk{
		"/home/u/.gradle": "dir", "/home/u/.gradle/wrapper/dists/x/bin": "dir",
		"/home/u/.npm": "dir", "/home/u/project/node_modules/.bin": "dir",
		"/tmp/tools/bin": "dir", "/opt/gradle": "dir", "/opt/gradle/bin": "dir",
		"/home/u/.cache/tools/bin": "dir",
	})
	in := sample(d)
	in.Path = []string{"/home/u/.gradle/wrapper/dists/x/bin", "/home/u/project/node_modules/.bin", "/tmp/tools/bin", "/usr/bin"}
	args := Args(in)
	got := strings.Join(args, " ")
	if index(args, "--bind", "/home/u/.gradle") >= 0 {
		t.Errorf("~/.gradle, which holds a PATH folder, is bound writable:\n%s", got)
	}
	for _, open := range []string{"/home/u/.npm", "/tmp", "/home/u/.cache"} {
		if index(args, "--bind", open) < 0 {
			t.Errorf("%s is not bound writable:\n%s", open, got)
		}
	}
	if !slices.Contains(writable(in), "/home/u/.gradle") {
		t.Errorf("writable, under any PATH, leaves out ~/.gradle: %v", writable(in))
	}
	want := Unused{Name: "the tool folder", Path: "/home/u/.gradle", Real: "/home/u/.gradle", PATHFolder: "/home/u/.gradle/wrapper/dists/x/bin"}
	if u := UnusedWriteAreas(in); len(u) != 1 || u[0] != want {
		t.Fatalf("UnusedWriteAreas = %+v, want [%+v]", u, want)
	}
	line := "the tool folder /home/u/.gradle is not used for writing: it holds the PATH folder /home/u/.gradle/wrapper/dists/x/bin, where a sandboxed command could put a program run later outside the sandbox: move the tool's bin out of the cache, or take it off PATH"
	if got := UnusedWriteAreas(in)[0].Line(); got != line {
		t.Errorf("Line =\n%s\nwant\n%s", got, line)
	}
	if got, want := Refusal(in, "/home/u/.gradle/caches/x", nil), "the sandbox refused a write to /home/u/.gradle/caches/x, which does not exist; "+line+"; that is a stop: tell the owner the path and why, never write it elsewhere"; got != want {
		t.Errorf("Refusal =\n%s\nwant\n%s", got, want)
	}
	// The cache itself, the same way; a variable that names a cache outside
	// the home changes nothing.
	in.Path = []string{"/home/u/.cache/tools/bin", "/opt/gradle/bin"}
	in.Env["GRADLE_USER_HOME"] = "/opt/gradle"
	args = Args(in)
	if index(args, "--bind", "/home/u/.cache") >= 0 || index(args, "--bind", "/opt/gradle") < 0 {
		t.Errorf("want ~/.cache unbound and /opt/gradle bound:\n%s", strings.Join(args, " "))
	}
}
