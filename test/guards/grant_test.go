// `jhon grant`: the owner's authorization of an ASK class for one project
// root, for a while. The safety guard and vcs-guard read the grants; only the
// owner writes them, with `! jhon grant`, which no tool hook sees.
//
// Every case runs the CLI in a child process with HOME and XDG_STATE_HOME in a
// scratch directory; the real grants directory is never touched.

package guards

import (
	"encoding/json"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

type grantSetup struct {
	dir, project, grants string
	env                  []string
}

func newGrantSetup(t *testing.T) grantSetup {
	t.Helper()
	dir := realTemp(t)
	s := grantSetup{dir: dir, project: filepath.Join(dir, "proj"), grants: filepath.Join(dir, "state", "jhon", "grants")}
	mkdir(t, filepath.Join(s.project, "src"))
	support.GitInit(t, s.project)
	s.env = support.Env("HOME="+filepath.Join(dir, "home"), "XDG_STATE_HOME="+filepath.Join(dir, "state"))
	return s
}

func (s grantSetup) grant(t *testing.T, cwd string, args ...string) support.Result {
	t.Helper()
	if cwd == "" {
		cwd = s.project
	}
	return support.Jhon(t, cwd, s.env, append([]string{"grant"}, args...)...)
}

func (s grantSetup) file(cls, root string) string {
	if root == "" {
		root = s.project
	}
	return filepath.Join(s.grants, rootKey(root), cls+".json")
}

type grantRecord struct {
	Class     string    `json:"class"`
	Root      string    `json:"root"`
	GrantedAt time.Time `json:"granted_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func readGrant(t *testing.T, path string) grantRecord {
	t.Helper()
	var r grantRecord
	if err := json.Unmarshal([]byte(support.Read(t, path)), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (s grantSetup) push(t *testing.T) int {
	t.Helper()
	return hook(t, s.env, "safety-guard", nil, map[string]any{"tool_name": "Bash", "cwd": s.project, "agent_id": "a1", "agent_type": "worker",
		"permission_mode": "bypassPermissions", "tool_input": map[string]any{"command": "git push"}}).Code
}

func near(d, want time.Duration) bool { return math.Abs(float64(d-want)) < float64(2*time.Second) }

func TestGrantWritesRecordForToplevel(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	before := time.Now()
	r := s.grant(t, filepath.Join(s.project, "src"), "push")
	code(t, r, 0, "grant push")
	rec := readGrant(t, s.file("push", ""))
	if rec.Class != "push" || rec.Root != s.project {
		t.Errorf("record = %+v", rec)
	}
	if ttl := rec.ExpiresAt.Sub(rec.GrantedAt); !near(ttl, 30*time.Minute) {
		t.Errorf("ttl = %v; want 30m", ttl)
	}
	if rec.GrantedAt.Before(before.Add(-time.Second)) {
		t.Errorf("granted_at %v before %v", rec.GrantedAt, before)
	}
	matches(t, r.Stdout, `granted push for .*proj until `, "stdout")
}

func TestGrantAllowsUntilRevoked(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	if c := s.push(t); c != 2 {
		t.Fatalf("push before the grant: %d", c)
	}
	code(t, s.grant(t, "", "push", "--for", "5m"), 0, "grant")
	if c := s.push(t); c != 0 {
		t.Fatalf("push granted: %d", c)
	}
	code(t, s.grant(t, "", "--revoke", "push"), 0, "revoke")
	if exists(s.file("push", "")) {
		t.Error("the record survives --revoke")
	}
	if c := s.push(t); c != 2 {
		t.Fatalf("push after revoke: %d", c)
	}
}

func TestGrantDurationsAndRoot(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	code(t, s.grant(t, "", "deploy", "--for", "2h"), 0, "deploy --for 2h")
	rec := readGrant(t, s.file("deploy", ""))
	if ttl := rec.ExpiresAt.Sub(rec.GrantedAt); !near(ttl, 2*time.Hour) {
		t.Errorf("ttl = %v; want 2h", ttl)
	}
	other := mkdir(t, filepath.Join(s.dir, "other"))
	support.GitInit(t, other)
	code(t, s.grant(t, s.dir, "remote", "--root", other), 0, "--root")
	if !exists(s.file("remote", other)) {
		t.Error("no record for --root")
	}
}

func TestGrantList(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	s.grant(t, "", "push")
	s.grant(t, "", "gh-write", "--for", "1h")
	r := s.grant(t, "", "--list")
	code(t, r, 0, "--list")
	matches(t, r.Stdout, `push\s+.*proj\s+live until `, "--list")
	matches(t, r.Stdout, `gh-write\s+.*proj\s+live until `, "--list")
}

func TestGrantUsageErrorsWriteNothing(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	for _, args := range [][]string{{"force-push"}, {"push", "--for", "soon"}, {"push", "--for", "0m"}, {}, {"push", "--root"}, {"--revoke"}} {
		code(t, s.grant(t, "", args...), 2, strings.Join(args, " "))
	}
	if exists(s.grants) {
		t.Error("a usage error wrote the grants directory")
	}
}

func TestGrantDispatch(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	code(t, s.grant(t, "", "publish"), 0, "grant publish")
	if !exists(s.file("publish", "")) {
		t.Error("no record")
	}
}

// Plan item 11: grants expired mid-run and the owner asked for one approval
// covering several classes ("ya di los grants", the 49dcc77e audit).
func TestGrantSeveralClasses(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	r := s.grant(t, "", "push", "gh-write", "deploy", "--for", "2h")
	code(t, r, 0, "grant several")
	expiries := map[time.Time]bool{}
	for _, c := range []string{"push", "gh-write", "deploy"} {
		expiries[readGrant(t, s.file(c, "")).ExpiresAt] = true
		matches(t, r.Stdout, `(?m)^granted `+regexp.QuoteMeta(c)+` for \S+ until \S+$`, "one line per class, as jhon audit reads it")
	}
	if len(expiries) != 1 {
		t.Errorf("one expiry for all: %v", expiries)
	}
	code(t, s.grant(t, "", "--revoke", "push", "deploy"), 0, "revoke several")
	if exists(s.file("push", "")) || exists(s.file("deploy", "")) || !exists(s.file("gh-write", "")) {
		t.Error("--revoke push deploy did not remove exactly those two")
	}
}

func TestGrantUnknownOrRepeatedClassWritesNothing(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	for _, args := range [][]string{{"push", "force-push"}, {"push", "push"}} {
		code(t, s.grant(t, "", args...), 2, strings.Join(args, " "))
		if exists(s.file("push", "")) {
			t.Errorf("%s wrote push", strings.Join(args, " "))
		}
	}
}
