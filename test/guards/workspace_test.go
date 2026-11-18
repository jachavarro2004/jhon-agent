// T57 slice 4 (L1299 W2): joining or leaving a multi-repository workspace
// is the owner's act, refused from every agent as `jhon grant|mode|ldd` are,
// and the pointers under jhon's state are kept from them as the grants are.

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
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
			// The pointers' folder is the owner's, by any spelling of the real
			// one: a link into it too (a jhon/workspace elsewhere is no state of
			// jhon's, L1307: TestWorkspaceFolderNamedLikeStateIsAProject).
			link := filepath.Join(realTemp(t), "ptr")
			symlink(t, mkdir(t, filepath.Join(state, "jhon", "workspace")), link)
			for _, base := range []string{"$XDG_STATE_HOME/jhon/workspace", state + "/jhon/workspace", link, "$XDG_STATE_HOME/jhon/work*"} {
				for _, c := range []string{"cp x " + base + "/k.json", "echo '{}' > " + base + "/k.json", "rm -f " + base + "/k.json"} {
					code(t, ownerCall(t, env, repo, "subagent", c), 2, mode+": "+c)
				}
			}
		})
	}
}

// A project whose own path has a jhon/workspace segment is a project like
// any other (L1307): only the real pointer folder, under jhon's state, is
// the owner's; writes there, a symlink's included, stay refused.
func TestWorkspaceFolderNamedLikeStateIsAProject(t *testing.T) {
	t.Parallel()
	scratch := realTemp(t)
	state := filepath.Join(scratch, "state")
	env := support.Env("HOME="+sgHome, "XDG_STATE_HOME="+state, "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	proj := mkdir(t, filepath.Join(scratch, "u", "jhon", "workspace", "proj"))
	support.Write(t, proj, map[string]string{"src/a.txt": "a\n", "README": "r\n"})
	support.GitRepo(t, proj)
	pointers := mkdir(t, filepath.Join(state, "jhon", "workspace"))
	symlink(t, pointers, filepath.Join(proj, "ptr"))
	call := func(caller, tool string, input map[string]any) support.Result {
		t.Helper()
		payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": proj, "session_id": "s", "permission_mode": "default"}
		if caller == "subagent" {
			payload["permission_mode"] = "bypassPermissions"
			payload["agent_id"], payload["agent_type"] = "a1", "worker"
		}
		return hook(t, env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, payload)
	}
	write := func(p string) map[string]any { return map[string]any{"file_path": p, "content": "b\n"} }
	for _, caller := range []string{"top-default", "subagent"} {
		for _, c := range []string{
			"echo b > src/a.txt", "mkdir -p src/b", "sed -i s/a/b/ src/a.txt", "rm src/a.txt",
			"bash -c 'cd " + proj + " && ls'",
			`python3 -c "print(open('` + proj + `/README').read())"`,
		} {
			r := call(caller, "Bash", cmd(c))
			code(t, r, 0, caller+": "+c)
			noDecision(t, r.Stdout, caller+": "+c)
		}
		r := call(caller, "Write", write(filepath.Join(proj, "src", "c.txt")))
		code(t, r, 0, caller+": Write src/c.txt")
		noDecision(t, r.Stdout, caller+": Write src/c.txt")
		// The real pointer folder stays the owner's, by any spelling.
		for _, c := range []string{
			"echo '{}' > " + pointers + "/k.json", "echo '{}' > $XDG_STATE_HOME/jhon/workspace/k.json",
			"echo '{}' > ptr/k.json", "cp src/a.txt ptr/", "rm -f ptr/k.json",
			"bash -c 'jhon workspace join ../x'", "bash -c 'jhon workspace leave'",
		} {
			code(t, call(caller, "Bash", cmd(c)), 2, caller+": "+c)
		}
		r = call(caller, "Write", write(filepath.Join(proj, "ptr", "k.json")))
		if d, _, _ := decision(t, r.Stdout); r.Code != 2 && d != "deny" {
			t.Errorf("%s: Write through the symlink into the pointer folder was not refused: %d %s%s", caller, r.Code, r.Stdout, r.Stderr)
		}
	}
}
