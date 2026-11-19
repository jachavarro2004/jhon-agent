package ldd

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T57 slice 7 (L1324): a change's members are the workspace members a
// tasks.md slice names (`Repo: <name>`) or its record holds a run of
// (`repos.<name>`); `jhon deliver check` in the primary judges each one's
// delivery with the member's rules and is not ready while one is not;
// `jhon status` shows them in one line and in `members`.

func membersOf(c map[string]any) any { return c["members"] }

// D1, D2: the primary's check lists each member's delivery and waits for it.
func TestWorkspaceDeliverCheckInThePrimaryJudgesItsMembers(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	recFile := filepath.Join(f.app, "ldd", "changes", "c", "execution-record.json")
	check := func() (support.Result, map[string]any) {
		t.Helper()
		r := f.run(t, f.app, "deliver", "check", "c", "--json")
		return r, object(t, r.Stdout)
	}
	waits := "workspace member lib is not ready: run `jhon deliver check c` in " + f.lib

	// No run of the member's yet: the primary waits for it.
	equal(t, f.run(t, f.app, "executions", "run", "c").Code, 0)
	r, c := check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["ready"], false)
	equal(t, c["reasons"], []any{waits})
	equal(t, membersOf(c), []any{map[string]any{"name": "lib", "ready": false,
		"reasons": []any{"no recorded test run: run jhon executions run c --repo lib (" + recFile + ", repos.lib)"}}})

	// The member's run, of its HEAD: both are ready.
	equal(t, f.run(t, f.app, "executions", "run", "c", "--repo", "lib").Code, 0)
	r, c = check()
	equal(t, r.Code, 0, r.Out())
	equal(t, c["reasons"], []any{})
	equal(t, membersOf(c), []any{map[string]any{"name": "lib", "ready": true, "reasons": []any{}}})
	r = f.run(t, f.app, "deliver", "check", "c")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^merge  ready\n(?s:.*)^member  lib  ready$`)

	// A commit in the member after its run: the member, so the primary, is not ready.
	support.Write(t, f.lib, map[string]string{"a/b.go": "package a\n"})
	commit(t, f.lib, "more")
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{waits})
	lib := membersOf(c).([]any)[0].(map[string]any)
	equal(t, lib["ready"], false)
	match(t, lib["reasons"].([]any)[0].(string), "^"+regexp.QuoteMeta("HEAD's tree is not the tested tree: 1 path(s) differ (a/b.go); test this revision"))
	r = f.run(t, f.app, "deliver", "check", "c")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stdout, `(?m)^  - `+regexp.QuoteMeta(waits)+`$`)
	match(t, r.Stdout, `(?m)^member  lib  not ready\n    - HEAD's tree is not the tested tree: 1 path\(s\) differ \(a/b\.go\)`)

	// D4: status shows the same, in one line and in --json.
	s := statusOf(t, f, f.app)
	equal(t, s["members"], membersOf(c))
	r = f.run(t, f.app, "status", "c")
	match(t, r.Stdout, `(?m)^members     lib not ready  \(`+"`jhon deliver check c`"+` in a member says why\)$`)
}

// D1: the record's repos entry makes a member too; a Repo: line naming no
// listed member does not, and a change with no member keeps today's output.
func TestWorkspaceChangeMembersFromTheRecord(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	support.Write(t, f.app, map[string]string{"ldd/changes/d/tasks.md": "## Slice 1: web\n\nRepo: web\n\n- [ ] web task\n"})
	commit(t, f.app, "d")
	equal(t, f.run(t, f.app, "executions", "run", "d").Code, 0)
	none := func() {
		t.Helper()
		r := f.run(t, f.app, "deliver", "check", "d", "--json")
		equal(t, r.Code, 0, r.Out())
		_, has := object(t, r.Stdout)["members"]
		equal(t, has, false, r.Stdout)
		equal(t, strings.Contains(f.run(t, f.app, "deliver", "check", "d").Stdout, "member"), false)
		_, has = statusOf2(t, f, "d")["members"]
		equal(t, has, false)
		equal(t, strings.Contains(f.run(t, f.app, "status", "d").Stdout, "members"), false)
	}
	none()

	equal(t, f.run(t, f.lib, "executions", "run", "d", "--repo", "lib").Code, 0)
	r := f.run(t, f.app, "deliver", "check", "d", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, membersOf(object(t, r.Stdout)), []any{map[string]any{"name": "lib", "ready": true, "reasons": []any{}}})
	match(t, f.run(t, f.app, "status", "d").Stdout, `(?m)^members     lib ready$`)
}

func statusOf2(t *testing.T, f wsFixture, change string) map[string]any {
	t.Helper()
	return object(t, f.run(t, f.app, "status", change, "--json").Stdout)
}

// D1, D2 (L1334): a member only the record names, whose run failed, holds
// the primary's delivery.
func TestWorkspaceRecordOnlyMemberNotReadyHoldsThePrimary(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	support.Write(t, f.app, map[string]string{"ldd/changes/d/tasks.md": "## Slice 1: core\n\n- [ ] core task\n"})
	commit(t, f.app, "d")
	equal(t, f.run(t, f.app, "executions", "run", "d").Code, 0)
	support.Write(t, f.lib, map[string]string{"a/a.go": "package a // bad\n"})
	commit(t, f.lib, "bad")
	equal(t, f.run(t, f.lib, "executions", "run", "d", "--repo", "lib").Code, 1)
	r := f.run(t, f.app, "deliver", "check", "d", "--json")
	equal(t, r.Code, 1, r.Out())
	c := object(t, r.Stdout)
	equal(t, membersOf(c), []any{map[string]any{"name": "lib", "ready": false, "reasons": []any{"the last test run did not pass: a"}}})
	equal(t, c["reasons"], []any{"workspace member lib is not ready: run `jhon deliver check d` in " + f.lib})
}

// D4 (L1334): a member that has not joined is named with the owner's join,
// not with a check it cannot run.
func TestWorkspaceUnjoinedMemberNamesTheJoin(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	equal(t, f.run(t, f.lib, "workspace", "leave").Code, 0)
	join := f.lib + " has not joined the workspace: the owner runs `jhon workspace join " + f.app + "` in it"
	r := f.run(t, f.app, "status", "c")
	match(t, r.Stdout, `(?m)^members     lib not ready  \(`+regexp.QuoteMeta(join)+`\)$`)
	r = f.run(t, f.app, "deliver", "check", "c", "--json")
	equal(t, object(t, r.Stdout)["reasons"].([]any)[1], "workspace member lib is not ready: "+join)
}
