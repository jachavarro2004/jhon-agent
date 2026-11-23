// T217 (odd/tasks/t217-deploy-tool-hint.md): a program the owner lists in
// sandbox.deploy_tools runs outside the sandbox only alone on its line. A
// work line that holds one ends with a note saying so, whether it failed or
// not, and `jhon sandbox status` run inside the sandbox says it is inside
// instead of failing to start another one. The fake aws (fakeAWS) writes
// outside every folder a sandboxed command may write, so it succeeds only
// unsandboxed; no real aws runs.
package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// deployNote is the note a work line that holds aws, of the listed tools,
// ends with; deployNotes the one for aws and cdk.
const (
	deployNote  = "aws runs outside the sandbox (sandbox.deploy_tools) only on a line of its own, called by its name, with no pipes, ;, $(…), ~, $VAR or VAR=value in front, or other commands: run it alone, as in aws <arguments>, and pass settings as options (--region, --profile), not variables\n"
	deployNotes = "aws and cdk run outside the sandbox (sandbox.deploy_tools) only on a line of their own, called by their names, with no pipes, ;, $(…), ~, $VAR or VAR=value in front, or other commands: run each alone, as in aws <arguments>, and pass settings as options (--region, --profile), not variables\n"
)

// ownersLine is the line the owner's agent ran (L1, L2).
const ownersLine = "export AWS_REGION=us-east-1; aws sts get-caller-identity --query Account --output text 2>&1 | tail -1"

// deployBox is a box with the fake aws first on PATH, its note, and a
// runner of `<bash> -c <line>` as the runtimes start it.
func deployBox(t *testing.T) (b box, note string, run func(line string) support.Result) {
	t.Helper()
	b = newBox(t)
	bash := realShell(t, "bash")
	note = filepath.Join(b.base, "aws-ran")
	fakeAWS(t, &b, filepath.Join(b.base, "aws-bin"), note)
	return b, note, func(line string) support.Result {
		return support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", bash, "-c", line)
	}
}

// S1: the owner's line, which exits 0, ends with the note once, on stderr,
// on a line of its own; its exit code is unchanged.
func TestAWorkLineWithADeployToolEndsWithTheNote(t *testing.T) {
	b, note, run := deployBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"aws\", \"cdk\", \"sam\"]\n")
	r := run(ownersLine)
	if r.Code != 0 || r.Stderr != deployNote || exists(note) {
		t.Errorf("the owner's line: exit %d, stderr %q, aws ran unsandboxed %v; want 0, in the sandbox, and stderr the note alone", r.Code, r.Stderr, exists(note))
	}
	// A failure keeps its exit code and its own stderr before the note.
	r = run("aws s3 ls 2>/dev/null | tail -1; echo oops >&2; exit 3")
	if r.Code != 3 || r.Stderr != "oops\n"+deployNote {
		t.Errorf("a failing line: exit %d, stderr %q; want 3 and the note after its stderr", r.Code, r.Stderr)
	}
	// Two listed tools: the plural note, in the setting's order.
	if r := run("cdk deploy 2>/dev/null | cat; aws s3 ls 2>/dev/null | cat"); r.Code != 0 || r.Stderr != deployNotes {
		t.Errorf("aws and cdk: exit %d, stderr %q; want 0 and the plural note", r.Code, r.Stderr)
	}
}

// S2: with deploy_tools unset the owner's line gives no note.
func TestAWorkLineWithAToolNotListedGivesNoNote(t *testing.T) {
	_, _, run := deployBox(t)
	if r := run(ownersLine); r.Code != 0 || r.Stderr != "" {
		t.Errorf("the owner's line with deploy_tools unset: exit %d, stderr %q; want 0 and no note", r.Code, r.Stderr)
	}
	if r := run("aws s3 ls 2>/dev/null; true"); r.Code != 0 || r.Stderr != "" {
		t.Errorf("aws s3 ls; true with deploy_tools unset: exit %d, stderr %q; want 0 and no note", r.Code, r.Stderr)
	}
}

// S1: a listed tool alone on its line runs unsandboxed with no note; a line
// with no listed program keeps today's output; a line a signal kills gets
// no note; the git/gh note is unchanged, and comes before the deploy note
// on a line that holds both.
func TestTheDeployNoteLeavesOtherLinesAsTheyAre(t *testing.T) {
	b, note, run := deployBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"aws\", \"cdk\", \"sam\"]\n")
	if r := run("aws sts get-caller-identity"); r.Code != 0 || r.Stderr != "" || !exists(note) {
		t.Errorf("aws alone: exit %d, stderr %q, ran unsandboxed %v; want 0, no note, unsandboxed", r.Code, r.Stderr, exists(note))
	}
	if r := run("echo ok | cat"); r.Code != 0 || r.Stdout != "ok\n" || r.Stderr != "" {
		t.Errorf("echo ok | cat: exit %d, stdout %q, stderr %q; want 0, ok and no note", r.Code, r.Stdout, r.Stderr)
	}
	if r := run("false"); r.Code != 1 || r.Stderr != "" {
		t.Errorf("false: exit %d, stderr %q; want 1 and no note", r.Code, r.Stderr)
	}
	if r := run("aws s3 ls 2>/dev/null | tail -1; kill -TERM $$"); r.Code != 143 || strings.Contains(r.Stderr, deployNote) {
		t.Errorf("a shell killed by SIGTERM: exit %d, stderr %q; want 143 and no note", r.Code, r.Stderr)
	}
	r := run("set -o pipefail; git fetch origin | cat")
	if r.Code == 0 || !strings.HasSuffix(r.Stderr, "\n"+deliveryNote) || strings.Count(r.Stderr, deliveryNote) != 1 || strings.Contains(r.Stderr, deployNote) {
		t.Errorf("git fetch origin | cat: exit %d, stderr %q; want a failure ending with the git/gh note alone", r.Code, r.Stderr)
	}
	r = run("aws s3 ls 2>/dev/null | tail -1; git fetch origin")
	if r.Code != 128 || !strings.HasSuffix(r.Stderr, "\n"+deliveryNote+deployNote) || strings.Count(r.Stderr, deployNote) != 1 {
		t.Errorf("aws and git fetch: exit %d, stderr %q; want 128 ending with the git/gh note, then the deploy note", r.Code, r.Stderr)
	}
}

// insideStatus is `jhon sandbox status --json` as the fields T217 reads.
type insideStatus struct {
	Mode       string `json:"mode"`
	ModeSource string `json:"mode_source"`
	CanRun     bool   `json:"can_run"`
	Inside     *bool  `json:"inside"`
	Reason     string `json:"reason"`
	Project    string `json:"project"`
}

// S4: `jhon sandbox status` run inside Jhon's sandbox says so and where the
// real state is, starts no sandbox, and its JSON has inside and can_run
// true; with no mode set the default reads as on. Outside, inside is false.
func TestStatusInsideTheSandboxSaysItIsInside(t *testing.T) {
	b := newBox(t)
	jhon := support.Bin(t)
	inside := func(args ...string) support.Result {
		return support.Jhon(t, b.proj, b.env, append([]string{"sandbox", "run", "--profile", "work", "--", jhon, "sandbox", "status"}, args...)...)
	}
	for _, c := range []struct{ settings, source string }{
		{"schema = 1\n\n[sandbox]\nmode = \"on\"\n", "settings"},
		{"schema = 1\n", "default"},
	} {
		b.settings(t, c.settings)
		want := "sandbox on; this command runs inside Jhon's sandbox, where another cannot start, so nothing was probed; jhon sandbox status run outside it (the owner's terminal) gives the real state; project " + b.proj + "\n"
		if r := inside(); r.Code != 0 || r.Stdout != want || r.Stderr != "" {
			t.Errorf("%s: status inside: exit %d, stdout %q, stderr %q; want 0 and %q", c.source, r.Code, r.Stdout, r.Stderr, want)
		}
		r := inside("--json")
		var s insideStatus
		if err := json.Unmarshal([]byte(r.Stdout), &s); r.Code != 0 || err != nil || s.Inside == nil || !*s.Inside || !s.CanRun || s.Mode != "on" || s.ModeSource != c.source || s.Reason != "" || s.Project != b.proj {
			t.Errorf("%s: status --json inside: exit %d, stdout %q, stderr %q; want inside and can_run true, mode on (%s)", c.source, r.Code, r.Stdout, r.Stderr, c.source)
		}
	}
	r := support.Jhon(t, b.proj, b.env, "sandbox", "status", "--json")
	var s insideStatus
	if err := json.Unmarshal([]byte(r.Stdout), &s); r.Code != 0 || err != nil || s.Inside == nil || *s.Inside || !s.CanRun {
		t.Errorf("status --json outside: exit %d, stdout %q; want inside false and can_run true", r.Code, r.Stdout)
	}
}

// The marker only changes what status reports: set by hand outside the
// sandbox, status says it can run, but a work run with no bubblewrap on PATH
// still runs nothing (T217 verify).
func TestASpoofedMarkerRunsNothingWithoutBubblewrap(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nmode = \"on\"\n")
	min := filepath.Join(b.base, "minbin")
	if err := os.MkdirAll(min, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"bash", "git"} {
		real, err := exec.LookPath(p)
		if err != nil {
			t.Skipf("skipped: no %s", p)
		}
		if err := os.Symlink(real, filepath.Join(min, p)); err != nil {
			t.Fatal(err)
		}
	}
	env := append(slices.DeleteFunc(slices.Clone(b.env), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") }), "PATH="+min, "JHON_IN_SANDBOX=1")
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--project", b.proj, "--profile", "work", "--", "bash", "-c", "echo ran")
	if r.Code != 126 || strings.Contains(r.Stdout, "ran") || !strings.Contains(r.Stderr, "bubblewrap (bwrap) is not on PATH, so nothing was run") {
		t.Errorf("a hand-set marker with no bubblewrap: exit %d, stdout %q, stderr %q; want 126 and nothing run", r.Code, r.Stdout, r.Stderr)
	}
}
