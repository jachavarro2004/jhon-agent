package ldd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T57 slice 4 (L1299, W2): a primary repository names its sibling
// repositories in its LDD config (`workspace:`, name: path relative to its
// top); a sibling joins with the owner's `jhon workspace join <primary>`,
// a pointer under jhon's state; `jhon status` reports the workspace and
// notes what does not hold together. Nothing else changes (slices 5-7).
func TestWorkspaceStatusJoinLeave(t *testing.T) {
	t.Parallel()
	dir := temp(t)
	state := filepath.Join(dir, "state")
	env := support.Env("XDG_STATE_HOME=" + state)
	repo := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return support.GitRepo(t, p)
	}
	app, lib, web, stray, notes := repo("app"), repo("lib"), repo("web"), repo("stray"), repo("notes")
	support.Write(t, notes, map[string]string{"sub/x": "x\n"})
	support.Write(t, app, map[string]string{"ldd/config.yaml": "delivery: change\nworkspace:\n  lib: ../lib\n  web: ../web  # the client\n" +
		"  docs: ../notes/sub\n  Bad_Name: ../lib\n  abs: /etc\nstrict_tdd: true\n"})
	run := func(dir string, args ...string) support.Result {
		t.Helper()
		return support.Jhon(t, dir, env, args...)
	}
	status := func(dir string) (string, map[string]any) {
		t.Helper()
		h, j := run(dir, "status"), run(dir, "status", "--json")
		return h.Stdout, object(t, j.Stdout)
	}
	member := func(name, path string, joined bool) any {
		return map[string]any{"name": name, "path": path, "joined": joined}
	}
	pointer := func(root string) string {
		sum := sha256.Sum256([]byte(root))
		return filepath.Join(state, "jhon", "workspace", hex.EncodeToString(sum[:])+".json")
	}
	unjoined := func(name, path string) string {
		return "workspace member " + name + " (" + path + ") has not joined: the owner runs `jhon workspace join " + app + "` in it"
	}
	fixed := []string{
		"workspace member docs: " + filepath.Join(notes, "sub") + " is not the top of a git repository; not followed",
		"workspace member name 'Bad_Name' is not [a-z0-9-]+; ignored",
		"workspace member abs: /etc is not a path relative to the primary's top; ignored",
	}
	hasNotes := func(s map[string]any, want ...string) {
		t.Helper()
		for _, n := range want {
			equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(n)+"$"), true, n, s["notes"])
		}
	}

	// Nobody joined yet: the primary lists its members, none joined.
	text, s := status(app)
	equal(t, s["workspace"], map[string]any{"role": "primary", "primary": app, "members": []any{
		member("lib", lib, false), member("web", web, false), member("docs", filepath.Join(notes, "sub"), false)}})
	hasNotes(s, append(fixed, unjoined("lib", lib), unjoined("web", web))...)
	match(t, text, `(?m)^workspace   primary  \(lib not joined, web not joined, docs not joined\)$`)
	ks := keys(t, []byte(run(app, "status", "--json").Stdout))
	equal(t, ks[len(ks)-1], "workspace", ks)

	// The owner joins lib, and a stray repository the primary does not list.
	r := run(lib, "workspace", "join", "../app")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "joined the workspace of "+app+": "+lib+"\n")
	var rec map[string]any
	if err := json.Unmarshal([]byte(support.Read(t, pointer(lib))), &rec); err != nil {
		t.Fatal(err)
	}
	equal(t, rec["root"], lib)
	equal(t, rec["primary"], app)
	r = run(stray, "workspace", "join", app)
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stderr, "jhon workspace: "+app+"'s LDD config lists no path to "+stray+"; it is no member until it does\n")

	text, s = status(app)
	equal(t, s["workspace"].(map[string]any)["members"], []any{
		member("lib", lib, true), member("web", web, false), member("docs", filepath.Join(notes, "sub"), false)})
	hasNotes(s, append(fixed, unjoined("web", web))...)
	equal(t, anyMatch(s["notes"], "^workspace member lib "), false, s["notes"])
	match(t, text, `(?m)^workspace   primary  \(lib joined, web not joined, docs not joined\)$`)

	// lib is a member; its status names the primary and the members.
	text, s = status(lib)
	equal(t, s["workspace"], map[string]any{"role": "member", "primary": app, "members": []any{
		member("lib", lib, true), member("web", web, false), member("docs", filepath.Join(notes, "sub"), false)}})
	match(t, text, `(?m)^workspace   member lib  \(primary `+regexp.QuoteMeta(app)+`\)$`)
	equal(t, anyMatch(s["notes"], "^workspace "), false, s["notes"])

	// A pointer whose primary does not list the repository: no member, a note.
	text, s = status(stray)
	_, has := s["workspace"]
	equal(t, has, false, s)
	hasNotes(s, "workspace pointer names "+app+", whose LDD config lists no path to this repository: not a member (the owner's `jhon workspace leave` removes the pointer)")
	noMatch(t, text, `(?m)^workspace `)

	// A listed sibling with no pointer: outside the workspace, no key, no note.
	text, s = status(web)
	_, has = s["workspace"]
	equal(t, has, false, s)
	equal(t, anyMatch(s["notes"], "workspace"), false, s["notes"])
	noMatch(t, text, `(?m)^workspace `)

	// list shows the pointers; leave removes this repository's.
	r = run(web, "workspace", "list")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^`+regexp.QuoteMeta(lib)+`  -> `+regexp.QuoteMeta(app)+`  \(joined \S+\)$`)
	match(t, r.Stdout, `(?m)^`+regexp.QuoteMeta(stray)+`  -> `+regexp.QuoteMeta(app)+`  \(joined \S+\)$`)
	r = run(web, "workspace", "list", "--json")
	equal(t, r.Code, 0, r.Out())
	var list []map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &list); err != nil {
		t.Fatal(err, r.Stdout)
	}
	equal(t, len(list), 2, list)

	r = run(lib, "workspace", "leave")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "left the workspace of "+app+": "+lib+"\n")
	equal(t, exists(pointer(lib)), false)
	_, s = status(lib)
	_, has = s["workspace"]
	equal(t, has, false, s)
	_, s = status(app)
	equal(t, s["workspace"].(map[string]any)["members"].([]any)[0], member("lib", lib, false))
	r = run(lib, "workspace", "leave")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "no workspace pointer for "+lib+"; nothing removed\n")
}

// The command's errors write nothing: a usage error exits 2, a folder that
// is no git repository (here or as the primary) and the primary itself 1.
func TestWorkspaceJoinRefusals(t *testing.T) {
	t.Parallel()
	dir := temp(t)
	state := filepath.Join(dir, "state")
	env := support.Env("XDG_STATE_HOME=" + state)
	lib, plain := filepath.Join(dir, "lib"), filepath.Join(dir, "plain")
	for _, p := range []string{lib, plain} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, lib)
	for _, c := range []struct {
		dir    string
		args   []string
		code   int
		stderr string
	}{
		{lib, []string{"workspace"}, 2, "usage: jhon workspace join <primary> | leave | list [--json]\n"},
		{lib, []string{"workspace", "join"}, 2, "usage: jhon workspace join <primary> | leave | list [--json]\n"},
		{lib, []string{"workspace", "join", "a", "b"}, 2, "usage: jhon workspace join <primary> | leave | list [--json]\n"},
		{lib, []string{"workspace", "rejoin"}, 2, "usage: jhon workspace join <primary> | leave | list [--json]\n"},
		{lib, []string{"workspace", "join", "../plain"}, 1, "jhon workspace: " + plain + " is no git repository\n"},
		{lib, []string{"workspace", "join", "."}, 1, "jhon workspace: a repository is no member of its own workspace\n"},
		{plain, []string{"workspace", "join", "../lib"}, 1, "jhon workspace: " + plain + " is no git repository\n"},
		{plain, []string{"workspace", "leave"}, 1, "jhon workspace: " + plain + " is no git repository\n"},
	} {
		r := support.Jhon(t, c.dir, env, c.args...)
		equal(t, r.Code, c.code, c.args, r.Out())
		equal(t, r.Stderr, c.stderr, c.args)
		equal(t, r.Stdout, "", c.args)
	}
	equal(t, exists(filepath.Join(state, "jhon", "workspace")), false)
	r := support.Jhon(t, lib, env, "workspace", "list")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "no workspace pointers\n")
}
