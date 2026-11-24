// T219 S5: on an unsandboxed git line every hook stays off but Jhon's merge
// gate, which git still runs for reference-transaction with Jhon's own
// values: a hook the repository's configuration plants for the event does
// not run, and the gate refuses a move of the default branch.
package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestTheMergeGateAloneRunsOnAnUnsandboxedLine(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.proj, map[string]string{"openspec/config.yaml": "schema: spec-driven\n"})
	support.Git(t, b.proj, "add", "-A")
	support.Git(t, b.proj, "commit", "-q", "-m", "ldd")
	remote := originOverSSH(t, &b, filepath.Join(b.base, "ssh-ran"))
	support.Git(t, b.proj, "checkout", "-q", "-b", "x")
	support.Git(t, b.proj, "commit", "-q", "--allow-empty", "-m", "x")
	support.Git(t, b.proj, "push", "-q", remote, "x")
	support.Git(t, b.proj, "checkout", "-q", "main")
	if r := support.Jhon(t, b.proj, b.env, "hook", "merge-gate", "--install"); r.Code != 0 {
		t.Fatalf("merge-gate --install: exit %d %s", r.Code, r.Out())
	}
	planted := filepath.Join(b.base, "planted-ran")
	support.Git(t, b.proj, "config", "hook.planted.event", "reference-transaction")
	support.Git(t, b.proj, "config", "hook.planted.command", "touch "+planted)
	before := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "main"))
	r := b.unsandboxed(t, b.env, "git pull -q --no-rebase origin x")
	if got := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "main")); got != before || !strings.Contains(r.Stderr, "jhon merge gate: ") {
		t.Errorf("git pull origin x unsandboxed: main %s (was %s), exit %d, stderr %q; want the gate's refusal", got, before, r.Code, r.Stderr)
	}
	if r := b.unsandboxed(t, b.env, "git fetch -q origin"); r.Code != 0 {
		t.Errorf("git fetch origin unsandboxed: exit %d %q", r.Code, r.Stderr)
	}
	if exists(planted) {
		t.Errorf("the planted reference-transaction hook ran on an unsandboxed line")
	}
}

// T219 S9: what `jhon sandbox run` runs carries the agent's marker, in the
// work profile and with no sandbox alike; the owner's own commands never
// pass through it.
func TestTheSandboxMarksTheAgentsCommands(t *testing.T) {
	b := newBox(t)
	r := b.sh(t, b.proj, "work", `printf %s "$JHON_AGENT"`)
	if r.Code != 0 || r.Stdout != "1" {
		t.Errorf("work profile: exit %d, stdout %q, stderr %q; want JHON_AGENT=1", r.Code, r.Stdout, r.Stderr)
	}
	support.Write(t, b.proj, map[string]string{"openspec/config.yaml": "schema: spec-driven\n"})
	support.Git(t, b.proj, "add", "-A")
	support.Git(t, b.proj, "commit", "-q", "-m", "ldd")
	if r := support.Jhon(t, b.proj, b.env, "hook", "merge-gate", "--install"); r.Code != 0 {
		t.Fatalf("merge-gate --install: exit %d %s", r.Code, r.Out())
	}
	before := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "main"))
	r = b.sh(t, b.proj, "work", "git -c user.name=t -c user.email=t@x commit -q --allow-empty -m agent")
	if got := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "main")); got != before || !strings.Contains(r.Stderr, "jhon merge gate: ") {
		t.Errorf("the agent's commit on main in the sandbox: main %s (was %s), exit %d %q; want the gate's refusal", got, before, r.Code, r.Stderr)
	}
	support.Git(t, b.proj, "commit", "-q", "--allow-empty", "-m", "owner")
	if got := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "main")); got == before {
		t.Errorf("the owner's commit on main did not run")
	}
}
