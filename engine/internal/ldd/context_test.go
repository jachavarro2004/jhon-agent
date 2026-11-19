package ldd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The context tests that call the package directly; the CLI is covered by
// the conformance tests under test/.

func gitRepoT(t *testing.T) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, root, "init", "-q")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	return root
}

func resolve(t *testing.T, root string, change *string, phase string) Context {
	t.Helper()
	c, err := ResolveContext(root, change, phase)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// keysOf is the context's keys in output order.
func keysOf(c Context) []string {
	var out []string
	for _, m := range c.object() {
		out = append(out, m.key)
	}
	return out
}

func TestContextDeclaration(t *testing.T) {
	jhonRoot(t)
	if got := PhaseContextFields(); !reflect.DeepEqual(got, map[string][]string{"explore": {"projects", "strict_tdd"}}) {
		t.Errorf("phase_context_fields = %v", got)
	}
	src := contract.MustContract("vocabulary.yaml")
	want := slices.Concat(contract.List(src, "phases"), contract.List(src, "project_phases"), []string{"none"})
	if NoPhase != "none" || !slices.Equal(ContextPhases(), want) {
		t.Errorf("phases = %v", ContextPhases())
	}
}

func TestResolveContext(t *testing.T) {
	jhonRoot(t)
	root := gitRepoT(t)
	c := resolve(t, root, nil, "none")
	head, _ := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if !slices.Equal(keysOf(c), ContextKeys) || c.Head == nil || *c.Head != strings.TrimSpace(string(head)) || c.ProjectRoot != root ||
		c.Change != nil || c.Phase != "none" || c.Vcs != statusOf(t, root, nil).Vcs || c.Vcs.State != "ready" {
		t.Errorf("ready: %+v", c)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(c.ContextID) {
		t.Errorf("context_id %s", c.ContextID)
	}
	if at, err := time.Parse(time.RFC3339Nano, c.ResolvedAt); err != nil || at.UTC().Format("2006-01-02T15:04:05.000Z") != c.ResolvedAt {
		t.Errorf("resolved_at %s", c.ResolvedAt)
	}
	ids := map[string]bool{}
	for range 5 {
		ids[resolve(t, root, nil, "none").ContextID] = true
	}
	if len(ids) != 5 {
		t.Error("every invocation has its own context_id")
	}

	explore := resolve(t, root, s("add-x"), "explore")
	st := statusOf(t, root, s("add-x"))
	if !slices.Equal(keysOf(explore), append(slices.Clone(ContextKeys), "projects", "strict_tdd")) ||
		!reflect.DeepEqual(explore.Fields[0].Value, st.Projects) || !reflect.DeepEqual(explore.Fields[1].Value, st.StrictTdd) || *explore.Change != "add-x" {
		t.Errorf("explore carries status's projects and strict_tdd: %+v", explore)
	}
	for _, phase := range []string{"apply", "verify", "archive", "init", "none"} {
		if got := keysOf(resolve(t, root, s("c"), phase)); !slices.Equal(got, ContextKeys) {
			t.Errorf("%s: %q", phase, got)
		}
	}
	os.MkdirAll(filepath.Join(root, "openspec/changes/only"), 0o755)
	if resolve(t, root, nil, "none").Change != nil {
		t.Error("the context never selects a change on its own")
	}
	if _, err := ResolveContext(root, nil, "deploy"); err == nil || !strings.Contains(err.Error(), "unknown phase 'deploy'") {
		t.Errorf("an unknown phase is refused: %v", err)
	}
}

func TestContextBelowReady(t *testing.T) {
	jhonRoot(t)
	noHead, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, noHead, "init", "-q")
	if c := resolve(t, noHead, nil, "none"); c.Vcs.State != "no_head" || c.Head != nil {
		t.Errorf("no_head: %+v", c)
	}
	plain, _ := filepath.EvalSymlinks(t.TempDir())
	c := resolve(t, plain, s("c"), "apply")
	if c.Vcs.State != "none" || c.Head != nil || c.ProjectRoot != plain || ContextRoot(plain) != plain {
		t.Errorf("none: %+v", c)
	}
	out, err := js.StringifyCompact(c)
	if err != nil || !strings.Contains(out, `"head":null,"project_root":"`+plain+`","change":"c","phase":"apply","vcs":{"state":"none"`) {
		t.Errorf("compact JSON: %v %s", err, out)
	}
}

func TestAreasOf(t *testing.T) {
	got := AreasOf([]Reason{
		{Medium, "services/Notifications/mailer.ts", ""}, {Medium, "src/.hidden/a.ts", ""}, {Medium, "notifications/x/y", ""},
		{Passive, "", "no change"}, {Medium, "a/b/c/d/e/f/g/h/i/j/k.ts", ""},
	})
	if want := []string{"notifications", "x", "a", "b", "c", "d", "e", "f"}; !slices.Equal(got, want) {
		t.Errorf("AreasOf = %q", got)
	}
}

// fakeEngram answers `engram search` with fixed rows and records its arguments.
func fakeEngram(t *testing.T) (bin, args string) {
	dir := t.TempDir()
	bin, args = filepath.Join(dir, "engram"), filepath.Join(dir, "args.txt")
	write(t, bin, `#!/bin/sh
printf '%s\n' "$*" >> '`+args+`'
case "$*" in
  *"--type decision"*) printf 'Found 2 memories:\n\n[1] #101 (decision) — Tenant isolation by search_path\n    body\n\n[2] #102 (decision) — Notification service owns email\r[3] #0101 (decision) — dup\n' ;;
  *) printf 'Found 1 memories:\n\n[1] #103 (architecture) — Event bus per service  \n    body\n' ;;
esac
`)
	os.Chmod(bin, 0o755)
	return bin, args
}

func TestHandoffContext(t *testing.T) {
	jhonRoot(t)
	root := gitRepoT(t)
	write(t, filepath.Join(root, "services/notifications/mailer.ts"), "export const send = () => 1;\n")
	bin, args := fakeEngram(t)
	t.Setenv("JHON_ENGRAM", bin)
	c, err := HandoffContext(resolve(t, root, s("hu-12-items"), "none"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keysOf(c), append(slices.Clone(ContextKeys), "risk", "decisions")) || c.Fields[0].Value.(Risk).Tier != High {
		t.Errorf("handoff: %+v", c)
	}
	want := []Decision{{101, "decision", "Tenant isolation by search_path"}, {102, "decision", "Notification service owns email"}, {103, "architecture", "Event bus per service"}}
	if !reflect.DeepEqual(c.Fields[1].Value, want) {
		t.Errorf("decisions = %+v", c.Fields[1].Value)
	}
	asked, _ := os.ReadFile(args)
	if !regexp.MustCompile(`(?m)^search hu-12-items notifications .*--type decision`).Match(asked) || !strings.Contains(string(asked), "--type architecture") {
		t.Errorf("asked %s", asked)
	}

	t.Setenv("JHON_ENGRAM", filepath.Join(root, "no-such-engram"))
	c, err = HandoffContext(resolve(t, gitRepoT(t), s("c1"), "none"))
	out, _ := js.StringifyCompact(c)
	var decoded map[string]any
	json.Unmarshal([]byte(out), &decoded)
	if err != nil || decoded["decisions"] != nil || !strings.HasPrefix(decoded["decisions_note"].(string), "engram unavailable") ||
		!strings.Contains(out, `"risk":{"tier":"passive","base":"HEAD","reasons":[]}`) {
		t.Errorf("engram unavailable: %v %s", err, out)
	}
}

// script is an executable shell script named engram in a fresh directory.
func script(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "engram")
	write(t, bin, "#!/bin/sh\n"+body)
	os.Chmod(bin, 0o755)
	return bin
}

// T163 (L1421, L1430): the handoff's decisions_note names why Engram could
// not be asked, and an Engram that answers with nothing is no failure.
func TestHandoffDecisionsNote(t *testing.T) {
	jhonRoot(t)
	root := gitRepoT(t)
	write(t, filepath.Join(root, "services/notifications/mailer.ts"), "export const send = () => 1;\n")
	const tail = "): search Engram for the change's decisions before handing off"
	for _, tc := range []struct {
		name, engram, note string
	}{
		{"exit with stderr", script(t, "printf '\\n  boom  \\nsecond line\\n' >&2; exit 1\n"), "engram unavailable (engram exited 1: boom" + tail},
		{"exit without stderr", script(t, "exit 3\n"), "engram unavailable (engram exited 3" + tail},
		{"long stderr", script(t, "printf '%0300d\\n' 0 >&2; exit 2\n"), "engram unavailable (engram exited 2: " + strings.Repeat("0", 200) + tail},
		{"missing", filepath.Join(root, "no-such-engram"), "engram unavailable (engram not found" + tail},
		{"under a file", "/dev/null/engram", "engram unavailable (engram not found" + tail},
		{"too much output", script(t, "head -c 1100000 /dev/zero | tr '\\0' x\n"), "engram unavailable (engram output over 1 MiB" + tail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JHON_ENGRAM", tc.engram)
			c, err := HandoffContext(resolve(t, root, s("hu-12-items"), "none"))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(keysOf(c), append(slices.Clone(ContextKeys), "risk", "decisions", "decisions_note")) ||
				c.Fields[1].Value != nil || c.Fields[2].Value != tc.note {
				t.Errorf("got %+v, want note %q", c.Fields, tc.note)
			}
		})
	}
	for _, tc := range []struct{ name, engram string }{
		{"no memories", script(t, "echo 'No memories found for: \"x\"'\n")},
		{"no output", script(t, "exit 0\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JHON_ENGRAM", tc.engram)
			c, err := HandoffContext(resolve(t, root, s("hu-12-items"), "none"))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(keysOf(c), append(slices.Clone(ContextKeys), "risk", "decisions")) ||
				!reflect.DeepEqual(c.Fields[1].Value, []Decision{}) {
				t.Errorf("got %+v, want decisions [] and no note", c.Fields)
			}
		})
	}
}

// Engram not on PATH, with JHON_ENGRAM unset, is not found.
func TestDecisionsWhyNotOnPath(t *testing.T) {
	t.Setenv("JHON_ENGRAM", "")
	t.Setenv("PATH", t.TempDir())
	if d, why := decisionsWithin(context.Background(), t.TempDir(), "q"); d != nil || why != "engram not found" {
		t.Errorf("no engram on PATH: %v %q", d, why)
	}
}

// A search that outlives its budget timed out.
func TestDecisionsWhyTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if d, why := decisionsWhy(ctx, script(t, "exec sleep 5\n"), t.TempDir(), "q"); d != nil || why != "engram timed out after 15s" {
		t.Errorf("slow engram: %v %q", d, why)
	}
}
