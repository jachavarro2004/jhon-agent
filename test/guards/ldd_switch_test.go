// The owner's acts (`jhon mode`, `jhon grant`, and the retired `jhon ldd
// on|off`) are refused from every agent, as is any write into jhon's
// state, through every wrapper and spelling (T57c, L475, L477).

package guards

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// ownerFixture is a repository in its own state, with the owner's mode set.
func ownerFixture(t *testing.T, mode string) (repo, state string, env []string) {
	t.Helper()
	scratch := realTemp(t)
	state = filepath.Join(scratch, "state")
	env = support.Env("HOME="+sgHome, "XDG_STATE_HOME="+state, "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	repo = mkdir(t, filepath.Join(scratch, "repo"))
	support.Write(t, repo, map[string]string{"x": "x\n"})
	support.GitRepo(t, repo)
	code(t, support.Jhon(t, repo, env, "mode", mode), 0, "jhon mode "+mode)
	return repo, state, env
}

// ownerCall is one Bash call through the plugin's pre-tool, by caller.
func ownerCall(t *testing.T, env []string, repo, caller, command string) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd(command), "cwd": repo, "session_id": "s", "permission_mode": "default"}
	switch caller {
	case "top-bypass":
		payload["permission_mode"] = "bypassPermissions"
	case "subagent":
		payload["permission_mode"] = "bypassPermissions"
		payload["agent_id"], payload["agent_type"] = "a1", "worker"
	}
	return hook(t, env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, payload)
}

// An agent's own mode, grant or LDD switch is refused through any wrapper
// that runs its operand (L477, A1): busybox runs its applet, script -c its
// command, as sh -c does.
func TestOwnerActsThroughWrappers(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, act := range []string{"jhon ldd off", "jhon mode yolo", "jhon grant push"} {
				for _, c := range []string{
					"busybox sh -c '" + act + "'",
					"busybox " + act,
					"script -qc '" + act + "' /dev/null",
					"script -c '" + act + "' /dev/null",
					"script -q --command '" + act + "' /dev/null",
					"script --command='" + act + "' /dev/null",
					"busybox script -qc '" + act + "' /dev/null",
					// L479: every runner that runs its operand.
					"busybox ash -c '" + act + "'",
					"ash -c '" + act + "'",
					"mksh -c '" + act + "'",
					"yash -c '" + act + "'",
					"posh -c '" + act + "'",
					"flock /tmp/l -c '" + act + "'",
					"flock -n /tmp/l " + act,
					"echo '" + act + "' | at now",
					"at now <<< '" + act + "'",
					"echo '" + act + "' | batch",
					"parallel ::: '" + act + "'",
					"parallel '" + act + "' ::: a",
					"busybox env " + act,
					"busybox nohup " + act,
					"script -q -c '" + act + "' /dev/null",
					"sh -c 'busybox " + act + "'",
					"script -E never -c '" + act + "' /dev/null",
					"script /dev/null -c '" + act + "'",
					"/usr/bin/busybox " + act,
				} {
					for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
						r := ownerCall(t, env, repo, caller, c)
						code(t, r, 2, mode+"/"+caller+": "+c)
					}
				}
			}
		})
	}
}

// No agent writes jhon's owner-only state, whatever spells its directory
// (L477, A2): $XDG_STATE_HOME, ${XDG_STATE_HOME}, ~/.local/state, or the
// absolute path, in every mode and for every caller.
func TestOwnerStateEverySpelling(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, state, env := ownerFixture(t, mode)
			for _, dir := range []string{"modes", "grants", "session-grants", "ldd-switch"} {
				for _, base := range []string{"$XDG_STATE_HOME", "${XDG_STATE_HOME}", state, `"$XDG_STATE_HOME"`, "${XDG_STATE_HOME:-x}"} {
					for _, c := range []string{"cp x " + base + "/jhon/" + dir + "/k.json", "echo '{}' > " + base + "/jhon/" + dir + "/k.json",
						"rm -f " + base + "/jhon/" + dir + "/k.json", "cd " + base + "/jhon && echo '{}' > " + dir + "/k.json"} {
						for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
							r := ownerCall(t, env, repo, caller, c)
							code(t, r, 2, mode+"/"+caller+": "+c)
						}
					}
				}
			}
			// With no XDG_STATE_HOME, jhon's state is ~/.local/state; its spellings are refused too.
			noXdg := make([]string, 0, len(env))
			for _, kv := range env {
				if !strings.HasPrefix(kv, "XDG_STATE_HOME=") {
					noXdg = append(noXdg, kv)
				}
			}
			for _, base := range []string{"~/.local/state", "$HOME/.local/state", "${HOME}/.local/state", "$XDG_STATE_HOME", "${XDG_STATE_HOME}"} {
				c := "cp x " + base + "/jhon/ldd-switch/k.json"
				code(t, ownerCall(t, noXdg, repo, "subagent", c), 2, mode+" without XDG_STATE_HOME: "+c)
			}
			// A cwd inside the state: a relative write lands there too.
			if err := os.MkdirAll(filepath.Join(state, "jhon", "modes"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
				code(t, ownerCall(t, env, filepath.Join(state, "jhon"), caller, "echo '{}' > modes/k.json"), 2, mode+"/"+caller+": a cwd inside the state")
			}
			// With XDG_STATE_HOME unset, ${XDG_STATE_HOME:-x} is x: a relative x/jhon/modes is no state of jhon's.
			r := ownerCall(t, noXdg, repo, "subagent", "mkdir -p x/jhon && echo '{}' > ${XDG_STATE_HOME:-x}/jhon/k.json")
			code(t, r, 0, mode+": ${XDG_STATE_HOME:-x} unset")
		})
	}
}

// What those runners run harmlessly stays allowed (L479): a benign command
// through each is judged as the command itself.
func TestBenignRunnersStayAllowed(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			support.Write(t, repo, map[string]string{"f": "x\n"})
			for _, c := range []string{"busybox ls", "busybox grep x f", "script -q /dev/null true", "busybox sh -c 'echo hi'",
				"flock f true", "parallel echo ::: a b"} {
				for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 0, mode+"/"+caller+": "+c)
					noDecision(t, r.Stdout, mode+"/"+caller+": "+c)
				}
			}
		})
	}
}

// A variable spelling of a path outside jhon's state gets the decision its
// absolute spelling gets (L479): outside-write, asked in normal, passed by
// yolo and super-yolo, for every caller.
func TestStateVariableOtherAppPinned(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, state, env := ownerFixture(t, mode)
			// Under /tmp the state directory is a scratch root, where every
			// write passes, so the pin holds only for a state outside it.
			if strings.HasPrefix(filepath.Clean(state)+"/", "/tmp/") {
				t.Skip("the fixture's state is under /tmp, a scratch root; run with TMPDIR outside /tmp")
			}
			for _, base := range []string{state, "$XDG_STATE_HOME", "${XDG_STATE_HOME}", "${XDG_STATE_HOME:-/nope}"} {
				c := "cp x " + base + "/other-app/k.json"
				for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
					r := ownerCall(t, env, repo, caller, c)
					label := mode + "/" + caller + ": " + c
					switch {
					case mode != "normal":
						code(t, r, 0, label)
						matches(t, modeNote(t, r.Stdout), `allowed by the owner's `+mode+` mode: outside-write: `, label)
					case caller == "top-default":
						code(t, r, 0, label)
						matches(t, askedWhy(t, r.Stdout, label), `(^|\n)outside-write: `, label)
					default:
						code(t, r, 2, label)
					}
				}
			}
		})
	}
}

// Nothing is read only up to a count (L479 review): what parallel runs from
// its 17th argument on, and what xargs runs from its 17th replaced or 65th
// item on, is judged as the first is, in every mode and for every caller.
func TestRunnersReadEveryItem(t *testing.T) {
	t.Parallel()
	items := func(n int, sep string) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "a" + strconv.Itoa(i)
		}
		return strings.Join(parts, sep)
	}
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, act := range []string{"jhon ldd off", "jhon mode yolo", "jhon grant push", "git push --force"} {
				for _, c := range []string{
					"parallel ::: " + items(16, " ") + " '" + act + "'",
					"parallel sh -c ::: " + items(16, " ") + " '" + act + "'",
					"printf '" + items(16, `\n`) + `\n` + act + `\n' | xargs -I{} sh -c '{}'`,
					"printf '" + items(64, `\n`) + `\n` + act + `\n' | xargs -n1 sh -c`,
					"printf '" + items(64, `\n`) + `\n` + act + `\n' | xargs -L1 sh -c`,
					"printf '" + items(64, `\n`) + `\n` + act + `\n' | xargs -L 1 sh -c`,
				} {
					for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
						r := ownerCall(t, env, repo, caller, c)
						code(t, r, 2, mode+"/"+caller+": "+c[:min(len(c), 40)]+"… "+act)
					}
				}
			}
			// Many harmless items stay allowed.
			for _, c := range []string{"parallel echo ::: " + items(20, " "), "printf '" + items(70, `\n`) + `\n' | xargs -n1 echo`} {
				for _, caller := range []string{"top-default", "top-bypass", "subagent"} {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 0, mode+"/"+caller+": "+c[:30])
					noDecision(t, r.Stdout, mode+"/"+caller+": "+c[:30])
				}
			}
		})
	}
}
