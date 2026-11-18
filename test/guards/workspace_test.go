// T57 slice 4 (L1299 W2): joining or leaving a multi-repository workspace
// is the owner's act, refused from every agent as `jhon grant|mode|ldd` are,
// and the pointers under jhon's state are kept from them as the grants are.

package guards

import (
	"path/filepath"
	"strings"
	"testing"
)

const workspaceAct = "a workspace pointer is the owner's act; no agent joins or leaves a workspace"

func TestWorkspaceJoinAndLeaveAreTheOwners(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, state, env := ownerFixture(t, mode)
			for _, c := range []string{
				"jhon workspace join ../app",
				"jhon workspace leave",
				"jhon workspace join --json ../app",
				"sh -c 'jhon workspace leave'",
				"busybox jhon workspace join ../app",
				"go run ./engine/cmd/jhon workspace join ../app",
				"/tmp/x workspace leave",
				`python3 -c 'import subprocess; subprocess.run(["x", "workspace", "join", "../app"])'`,
			} {
				for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 2, mode+"/"+caller+": "+c)
				}
			}
			r := ownerCall(t, env, repo, "subagent", "jhon workspace join ../app")
			if !strings.Contains(r.Out(), workspaceAct) {
				t.Errorf("%s: the refusal does not name the owner's act: %s", mode, r.Out())
			}
			// Reading the pointers changes none.
			for _, c := range []string{"jhon workspace list", "jhon workspace list --json", "jhon workspace --help"} {
				for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 0, mode+"/"+caller+": "+c)
					noDecision(t, r.Stdout, mode+"/"+caller+": "+c)
				}
			}
			// The pointers' folder is the owner's, wherever a jhon state is.
			other := filepath.Join(realTemp(t), "jhon", "workspace")
			for _, base := range []string{"$XDG_STATE_HOME/jhon/workspace", state + "/jhon/workspace", other, "$XDG_STATE_HOME/jhon/work*"} {
				for _, c := range []string{"cp x " + base + "/k.json", "echo '{}' > " + base + "/k.json", "rm -f " + base + "/k.json"} {
					code(t, ownerCall(t, env, repo, "subagent", c), 2, mode+": "+c)
				}
			}
		})
	}
}
