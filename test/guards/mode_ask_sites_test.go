// Every ask site honours the owner's mode (S31, L154): "YOLO solo debe
// pedir para el destroy, SUPER YOLO para nada y normal para todo." Each site
// below is put, under normal, yolo and super-yolo, by the top-level session
// (where the owner can be asked) and by a sub-agent (where nobody can): a
// class the mode passes is allowed with the note and a line in the modes'
// log; any other is asked at the top level and refused from a sub-agent.
// What is refused stays refused in every mode (TestModeEveryModeStillRefuses).
//
// A new ask site, or a new class, fails engine/internal/hook's
// TestAskSitesAreListed until it is listed here.

package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// modePasses is which modes pass each ask class without asking. The
// grantable classes follow the owner's rule; review is the L154 bug; the
// questions no grant answers follow the decision recorded in
// grant.Questions (unreadable and unknown may hide a destroy; settings and
// git-config could switch the guards off).
var modePasses = map[string][]string{
	"push":       {"yolo", "super-yolo"},
	"gh-write":   {"yolo", "super-yolo"},
	"publish":    {"yolo", "super-yolo"},
	"remote":     {"yolo", "super-yolo"},
	"deploy":     {"yolo", "super-yolo"},
	"destroy":    {"super-yolo"},
	"review":     {"yolo", "super-yolo"},
	"unreadable": {"super-yolo"},
	"unknown":    {"super-yolo"},
	"settings":   nil,
	"git-config": nil,
}

// askSite is one place a guard asks the owner: its guard, the class it
// asks, and a call that reaches it, by the top level or by a sub-agent, in
// a fresh project whose mode the owner set.
type askSite struct {
	guard, cls, label string
	setup             func(t *testing.T) (state string, call func(top bool) support.Result, setMode func(mode string))
}

func safetySite(cls, command string) askSite {
	return askSite{guard: "safety-guard", cls: cls, label: command, setup: func(t *testing.T) (string, func(bool) support.Result, func(string)) {
		s := newSG(t)
		call := func(top bool) support.Result {
			if top {
				return s.bash(t, command, sgOpts{top: true})
			}
			return s.bash(t, command, sgOpts{})
		}
		return s.state, call, func(m string) { s.setMode(t, s.p, m) }
	}}
}

func vcsSite(cls, label string, file func(project string) string) askSite {
	return askSite{guard: "vcs-guard", cls: cls, label: label, setup: func(t *testing.T) (string, func(bool) support.Result, func(string)) {
		w := newWriteEnv(t)
		call := func(top bool) support.Result {
			payload := map[string]any{"tool_name": "Write", "cwd": w.project, "session_id": "s", "permission_mode": "default", "tool_input": map[string]any{"file_path": file(w.project)}}
			if !top {
				payload["agent_id"], payload["agent_type"], payload["permission_mode"] = "a1", "worker", "bypassPermissions"
			}
			return hook(t, w.env, "vcs-guard", vcsDirs(t), payload)
		}
		return w.state, call, func(m string) { code(t, support.Jhon(t, w.project, w.env, "mode", m), 0, "jhon mode "+m) }
	}}
}

func routeSite(label, kind string, prepare func(t *testing.T, r rgEnv)) askSite {
	return askSite{guard: "route-guard", cls: "review", label: label, setup: func(t *testing.T) (string, func(bool) support.Result, func(string)) {
		r := newRG(t, "")
		if prepare != nil {
			prepare(t, r)
		}
		call := func(top bool) support.Result {
			return r.call(t, "Agent", map[string]any{"subagent_type": kind, "prompt": "x"}, top)
		}
		return filepath.Dir(filepath.Dir(r.grants)), call, func(m string) { code(t, support.Jhon(t, r.p, r.env, "mode", m), 0, "jhon mode "+m) }
	}}
}

// askSites is every ask site in the guards, one call reaching each.
var askSites = []askSite{
	// route-guard: a reviewer the owner did not ask for (routeguard.go, ask "review").
	routeSite("a reviewer nobody asked for", "review-risk", nil),
	routeSite("a Judgment Day judge nobody asked for", "jd-judge-a", nil),
	// route-guard: a 4R lens the diff does not call for.
	routeSite("a 4R lens that is not pertinent", "review-readability", func(t *testing.T, r rgEnv) {
		support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "create table a (id int);\n", "tests/test_app.py": "def test_a(): pass\n",
			"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	}),
	// vcs-guard (vcsJudge).
	vcsSite("settings", "a write to the Claude Code settings", func(p string) string { return filepath.Join(p, ".claude", "settings.json") }),
	// safety-guard (guard.Classify), one command per class.
	safetySite("push", "git push origin main"),
	safetySite("gh-write", "gh pr merge 1 --squash"),
	safetySite("publish", "npm publish"),
	safetySite("remote", "ssh host"),
	safetySite("deploy", "kubectl apply -f x.yaml"),
	safetySite("destroy", "git reset --hard"),
	safetySite("unreadable", "echo (pwd)"),
	safetySite("unknown", "rm -f /tmp/$X"),
	safetySite("git-config", "git config core.hooksPath /tmp/h"),
}

func TestModeEveryAskSiteHonoursTheMode(t *testing.T) {
	t.Parallel()
	covered := map[string]bool{}
	for _, site := range askSites {
		covered[site.cls] = true
		for _, mode := range []string{"normal", "yolo", "super-yolo"} {
			t.Run(site.guard+"/"+site.cls+"/"+mode+"/"+site.label, func(t *testing.T) {
				t.Parallel()
				state, call, setMode := site.setup(t)
				setMode(mode)
				passes := slices.Contains(modePasses[site.cls], mode)
				for _, top := range []bool{true, false} {
					who := map[bool]string{true: "top level", false: "sub-agent"}[top]
					r := call(top)
					switch {
					case passes:
						code(t, r, 0, who)
						matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's `+regexp.QuoteMeta(mode)+` mode: `+regexp.QuoteMeta(site.cls)+`: `, who)
					case top:
						code(t, r, 0, who)
						matches(t, askedWhy(t, r.Stdout, who), `^`+regexp.QuoteMeta(site.cls)+`: `, who)
					default:
						code(t, r, 2, who)
						noDecision(t, r.Stdout, who)
					}
				}
				var logged []string
				if b, err := os.ReadFile(filepath.Join(state, "jhon", "modes", "allowed.jsonl")); err == nil {
					for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
						var a map[string]any
						if err := json.Unmarshal([]byte(l), &a); err != nil {
							t.Fatal(err)
						}
						logged = append(logged, a["mode"].(string)+" "+a["class"].(string)+" "+a["guard"].(string))
					}
				}
				want := ""
				if passes {
					line := mode + " " + site.cls + " " + site.guard
					want = line + ", " + line
				}
				equalStr(t, strings.Join(logged, ", "), want, "the modes' log")
			})
		}
	}
	for cls := range modePasses {
		if !covered[cls] {
			t.Errorf("class %s has no ask site in askSites", cls)
		}
	}
}

// A question no grant answers (a deletion it cannot resolve) is passed only when
// the session's own project's mode covers it too: a cd into a super-yolo
// project does not lend a normal session that mode (T46 verify, L157).
func TestModeUnreadableTakesTheSessionProjectToo(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	code(t, support.Jhon(t, s.other, s.env, "mode", "super-yolo"), 0, "mode other")
	r := s.bash(t, s.expand("cd <OTHER> && rm -f \"$X/config\""), sgOpts{})
	code(t, r, 2, "a normal session's unresolved deletion run in a super-yolo project")
	noDecision(t, r.Stdout, "sub-agent")
}
