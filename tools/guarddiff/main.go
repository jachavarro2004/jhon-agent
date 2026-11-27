// Command guarddiff is the guards' verdict differential (T128f): it feeds
// every line of the checked-in set (test/guards/corpus) to the guards of two
// jhon binaries, as Claude Code calls them, and prints each verdict that
// differs. See README.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// guards are the guards judged, in pre-tool's order (judge); an
// inspector's are those the Claude Code render gives a Prism facet.
var (
	guards          = []string{"safety-guard", "vcs-guard", "framework-guard", "secrets-guard", "scope-guard", "route-guard"}
	inspectorGuards = []string{"inspect-guard", "safety-guard", "framework-guard", "secrets-guard", "route-guard"}
)

func run(args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, "guarddiff:", err); return 2 }
	root := repoRoot()
	if len(args) > 0 && args[0] == "gen-fuzz" {
		out := filepath.Join(root, "test", "guards", "corpus", "fuzz.tsv")
		if err := os.WriteFile(out, []byte(fuzzTSV()), 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "guarddiff: wrote", out)
		return 0
	}
	fs := flag.NewFlagSet("guarddiff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	oldBin := fs.String("old", "", "the jhon binary judged first (the last release)")
	newBin := fs.String("new", "", "the jhon binary judged second (the candidate)")
	oldRoot := fs.String("old-root", root, "JHON_ROOT for --old: the tree it was built from")
	newRoot := fs.String("new-root", root, "JHON_ROOT for --new")
	linesDir := fs.String("lines", filepath.Join(root, "test", "guards", "corpus"), "the directory of the line set")
	allowPath := fs.String("allow", "", "the allow list of changed verdicts (TSV: id, guard, old, new, why)")
	scratch := fs.String("scratch", "", "where the fixtures are built (default: the user cache directory)")
	jobs := fs.Int("jobs", runtime.NumCPU(), "calls in parallel")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *oldBin == "" || *newBin == "" {
		return fail(fmt.Errorf("--old and --new name the two jhon binaries"))
	}
	lines, err := loadLines(*linesDir)
	if err != nil {
		return fail(err)
	}
	allow, err := loadAllow(*allowPath)
	if err != nil {
		return fail(err)
	}
	d, err := newDiff(*linesDir, *scratch, []*binary{{name: "old", path: *oldBin, root: *oldRoot}, {name: "new", path: *newBin, root: *newRoot}})
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(d.dir)
	verdicts, err := d.judgeAll(lines, max(*jobs, 1))
	if err != nil {
		return fail(err)
	}
	var changes []change
	count := map[string]int{}
	for _, j := range verdicts {
		count[j.v[1][0]]++
		for i, v := range j.v {
			if v[0] == "error" {
				fmt.Fprintf(stderr, "guarddiff: %s %s: the %s binary's call failed: %s\n", j.id, j.guard, d.bins[i].name, v[1])
			}
		}
		if j.v[0][0] != j.v[1][0] {
			changes = append(changes, change{id: j.id, guard: j.guard, old: j.v[0][0], new: j.v[1][0], reason: cmp(j.v[1][1], j.v[0][1])})
		}
	}
	refused := 0
	for _, c := range changes {
		mark := "allowed"
		if !allow.covers(c) {
			mark, refused = "NOT ALLOWED", refused+1
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s -> %s\t%s\t%s\n", c.id, c.guard, c.old, c.new, mark, c.reason)
	}
	fmt.Fprintf(stdout, "guarddiff: %d guard verdicts per binary; the new binary's: %d allow, %d ask, %d refuse, %d error\n",
		len(verdicts), count["allow"], count["ask"], count["refuse"], count["error"])
	plural := "s"
	if len(changes) == 1 {
		plural = ""
	}
	fmt.Fprintf(stdout, "guarddiff: %d lines, %d changed verdict%s (%d allowed, %d not on the allow list)\n", len(lines), len(changes), plural, len(changes)-refused, refused)
	if refused > 0 {
		return 1
	}
	return 0
}

// repoRoot is the nearest directory above the working one holding go.mod.
func repoRoot() string {
	dir, _ := os.Getwd()
	for d := dir; filepath.Dir(d) != d; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return dir
}

// line is one tool call: its tool and input (placeholders unexpanded), the
// fixture tree it runs in (place, fixture), its caller and working directory.
type line struct{ id, place, fixture, caller, tool, input, cwd string }

var (
	places  = []string{"ldd", "ldd:yolo", "ldd:super-yolo", "repo", "repo:yolo", "repo:super-yolo", "folder"}
	callers = []string{"top-default", "top-bypass", "subagent"}
	// workingSetPlaces are named only explicitly (`*` is places): a primary
	// under LDD listing its joined member <LIB>, a sibling <SITE> that joined
	// but is not listed, a repository <SHARED> only sandbox.allow_write
	// names, and a linked worktree <WT> on its own change (T281).
	workingSetPlaces = []string{"ws", "ws:super-yolo"}
	// inspector is a Prism facet, named only explicitly (`*` is callers): its
	// Bash is inspect-guard's too (T271).
	inspector = "inspector"
)

// loadLines reads every *.tsv under dir: a corpus line (8 columns, see
// test/guards/corpus_test.go) runs as its fixture sets it up; a guarddiff
// line has 6 columns: place, fixture, caller, tool, input (Go-quoted),
// source; a place or caller is one name, a comma list, or `*` for each.
func loadLines(dir string) ([]line, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.tsv"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no line files under %s", dir)
	}
	var out []line
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for n, text := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
				continue
			}
			at := filepath.Base(f) + ":" + strconv.Itoa(n+1)
			col := strings.Split(text, "\t")
			if len(col) != 8 && len(col) != 6 {
				return nil, fmt.Errorf("%s: %d columns, want 8 or 6", at, len(col))
			}
			input, err := strconv.Unquote(col[map[bool]int{true: 2, false: 4}[len(col) == 8]])
			if err != nil {
				return nil, fmt.Errorf("%s: the input is not Go-quoted: %v", at, err)
			}
			if len(col) == 8 {
				l := line{id: at, place: "corpus", fixture: col[4], caller: "subagent", tool: col[1], input: input}
				if col[0] == "safety-guard" {
					l.caller = "top-default"
				}
				if strings.HasPrefix(l.fixture, "framework") {
					l.fixture, l.cwd = "none", map[bool]string{true: "<FW>"}[col[4] == "framework-cwd"]
				}
				out = append(out, l)
				continue
			}
			for _, p := range expand(col[0], places) {
				for _, c := range expand(col[2], callers) {
					if base, _, _ := strings.Cut(p, ":"); !slices.Contains(places, p) && base != "corpus" && !slices.Contains(workingSetPlaces, p) || !slices.Contains(callers, c) && c != inspector {
						return nil, fmt.Errorf("%s: unknown place %q or caller %q", at, p, c)
					}
					out = append(out, line{id: at + "@" + p + "/" + c, place: p, fixture: col[1], caller: c, tool: col[3], input: input})
				}
			}
		}
	}
	return out, nil
}

func expand(field string, all []string) []string {
	if field == "*" {
		return all
	}
	return strings.Split(field, ",")
}

// change is one guard's verdict on one line that differs between binaries.
type change struct{ id, guard, old, new, reason string }

// allowList is the changed verdicts the owner accepted; `*` matches any value.
type allowList [][4]string

func loadAllow(path string) (allowList, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out allowList
	for n, text := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		col := strings.Split(text, "\t")
		if len(col) < 4 {
			return nil, fmt.Errorf("%s:%d: want id, guard, old, new and why", path, n+1)
		}
		out = append(out, [4]string(col[:4]))
	}
	return out, nil
}

func (a allowList) covers(c change) bool {
	got := [4]string{c.id, c.guard, c.old, c.new}
	return slices.ContainsFunc(a, func(e [4]string) bool {
		for i := range e {
			if e[i] != "*" && e[i] != got[i] {
				return false
			}
		}
		return true
	})
}

// binary is one jhon binary, the tree it reads (JHON_ROOT), and the tools
// each of its guards judges (`jhon hook pre-tool --matchers`).
type binary struct {
	name, path, root string
	matchers         map[string][]string
	vcsDirs          []string
	// inspect is its tree's inspect_allow (prose/agents.yaml), inspect-guard's arguments.
	inspect []string
}

type diff struct {
	dir, settings string
	trees         map[string]string // name of the fixture -> its files, from trees.txt
	bins          []*binary
	mu            sync.Mutex
	built         map[string]*treeOnce
}

type treeOnce struct {
	once sync.Once
	t    *tree
	err  error
}

func newDiff(linesDir, scratch string, bins []*binary) (*diff, error) {
	if scratch == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		scratch = filepath.Join(c, "jhon", "guarddiff")
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(scratch, "run-")
	if err != nil {
		return nil, err
	}
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		return nil, err
	}
	d := &diff{dir: dir, settings: filepath.Join(dir, "settings.toml"), bins: bins, built: map[string]*treeOnce{}}
	// The sandbox is no guard: off for both, so a verdict is the guards' alone.
	if err := os.WriteFile(d.settings, []byte("[sandbox]\nmode = \"off\"\n"), 0o644); err != nil {
		return d, err
	}
	if d.trees, err = readTrees(filepath.Join(linesDir, "trees.txt")); err != nil {
		return d, err
	}
	vcs := regexp.MustCompile(`(?m)^vcs_dirs:\s*\[(.*)\]`)
	for _, b := range bins {
		out, err := exec.Command(b.path, "hook", "pre-tool", "--matchers").Output()
		if err != nil {
			return d, fmt.Errorf("%s hook pre-tool --matchers: %v", b.path, err)
		}
		b.matchers = map[string][]string{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			g, tools, _ := strings.Cut(l, "\t")
			b.matchers[g] = strings.Split(tools, "|")
		}
		voc, err := os.ReadFile(filepath.Join(b.root, "prose", "contracts", "vocabulary.yaml"))
		m := vcs.FindSubmatch(voc)
		if err != nil || m == nil {
			return d, fmt.Errorf("%s: no vcs_dirs in prose/contracts/vocabulary.yaml: %v", b.root, err)
		}
		for _, s := range strings.Split(string(m[1]), ",") {
			b.vcsDirs = append(b.vcsDirs, strings.TrimSpace(s))
		}
		agents, _ := os.ReadFile(filepath.Join(b.root, "prose", "agents.yaml"))
		if m := inspectAllow.FindSubmatch(agents); m != nil {
			for _, s := range strings.Split(string(m[1]), ",") {
				b.inspect = append(b.inspect, strings.TrimSpace(s))
			}
		}
	}
	return d, nil
}

var inspectAllow = regexp.MustCompile(`(?m)^inspect_allow:\s*\[(.*)\]`)

// readTrees reads the fixture files: a `-- <fixture> <path> --` header, then
// the file's text; <path> is under the tree (proj/, other/, home/ …).
func readTrees(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out, cur := map[string]string{}, ""
	for _, l := range strings.SplitAfter(string(b), "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSuffix(l, "\n"), "-- "); ok && strings.HasSuffix(h, " --") {
			cur = strings.TrimSuffix(h, " --")
			out[cur] = ""
		} else if cur != "" {
			out[cur] += l
		}
	}
	return out, nil
}

// judgeAll is every guard's verdict on every line, in the lines' order.
func (d *diff) judgeAll(lines []line, jobs int) ([]judged, error) {
	per := make([][]judged, len(lines))
	errs := make([]error, len(lines))
	next := make(chan int)
	var wg sync.WaitGroup
	for range jobs {
		wg.Go(func() {
			for i := range next {
				per[i], errs[i] = d.judge(lines[i])
			}
		})
	}
	for i := range lines {
		next <- i
	}
	close(next)
	wg.Wait()
	var out []judged
	for i := range lines {
		if errs[i] != nil {
			return nil, fmt.Errorf("%s: %v", lines[i].id, errs[i])
		}
		out = append(out, per[i]...)
	}
	return out, nil
}

// judged is one guard's verdict and reason on one line, per binary.
type judged struct {
	id, guard string
	v         [2][2]string
}

func (d *diff) takes(guard, tool string) bool {
	return slices.ContainsFunc(d.bins, func(b *binary) bool { return slices.Contains(b.matchers[guard], tool) })
}

// judge runs l through every guard that takes its tool, in both binaries:
// first all of them in one pre-tool call, as Claude Code runs them; when it
// does not allow, each guard alone, route-guard through `jhon hook
// route-guard`.
func (d *diff) judge(l line) ([]judged, error) {
	t, err := d.tree(l.place, l.fixture)
	if err != nil {
		return nil, err
	}
	payload, err := t.payload(l)
	if err != nil {
		return nil, err
	}
	var out []judged
	gs := guards
	if l.caller == inspector {
		gs = inspectorGuards
	}
	for _, g := range gs {
		if d.takes(g, l.tool) {
			out = append(out, judged{id: l.id, guard: g})
		}
	}
	for i, b := range d.bins {
		all := [2]string{"allow", ""}
		if len(out) > 0 {
			all = t.call(b, t.args(b, out...), payload)
		}
		for j := range out {
			out[j].v[i] = all
			if all[0] != "allow" && len(out) > 1 {
				out[j].v[i] = t.call(b, t.args(b, out[j]), payload)
			}
		}
	}
	return out, nil
}

// cmp is a, else b.
func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// call is the verdict (allow, ask, refuse or error) of one hook call and
// its reason, with the tree's path written <SCRATCH>.
func (t *tree) call(b *binary, args []string, payload []byte) [2]string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, b.path, args...)
	c.Dir, c.Env, c.Stdin = t.proj, t.env(b), bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	verdict, reason := "allow", ""
	var j struct {
		Out struct {
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	switch code := c.ProcessState.ExitCode(); {
	case code == 2:
		verdict, reason = "refuse", stderr.String()
	case code != 0 || err != nil:
		verdict, reason = "error", fmt.Sprintf("exit %d: %v: %s", code, err, stderr.String())
	case json.Unmarshal(stdout.Bytes(), &j) == nil && j.Out.Decision != "" && j.Out.Decision != "allow":
		verdict, reason = map[string]string{"ask": "ask"}[j.Out.Decision], j.Out.Reason
		if verdict == "" {
			verdict = "refuse"
		}
	}
	reason, _, _ = strings.Cut(strings.ReplaceAll(reason, t.dir, "<SCRATCH>"), "\n")
	if r := []rune(reason); len(r) > 240 {
		reason = string(r[:240]) + "…"
	}
	return [2]string{verdict, reason}
}

// tree is one fixture: proj (the project), other (a repository beside it),
// tmpdir, home (with the framework's install), a transcript for the
// sub-agent caller, and a state directory per binary holding its mode.
type tree struct {
	dir, proj, home, fw, settings string
}

const scopeAgent = "a0123456789abcdef"

func (d *diff) tree(place, fixture string) (*tree, error) {
	d.mu.Lock()
	o := d.built[place+"|"+fixture]
	if o == nil {
		o = &treeOnce{}
		d.built[place+"|"+fixture] = o
	}
	d.mu.Unlock()
	o.once.Do(func() { o.t, o.err = d.build(place, fixture) })
	return o.t, o.err
}

func (d *diff) build(place, fixture string) (*tree, error) {
	dir, err := os.MkdirTemp(d.dir, "tree-")
	if err != nil {
		return nil, err
	}
	t := &tree{dir: dir, proj: filepath.Join(dir, "proj"), home: filepath.Join(dir, "home"), settings: d.settings}
	t.fw = filepath.Join(t.home, ".agents", "jhon")
	base, mode, _ := strings.Cut(place, ":")
	files := map[string]string{}
	for key, body := range d.trees {
		if tag, rel, _ := strings.Cut(key, " "); tag == "all" || tag == fixture || tag == base {
			files[rel] = t.expand(body)
		}
	}
	prompt, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user",
		"content": "Apply task 1.1.\n\n## Allowed edit surfaces\n- `src/a.ts`\n- src/parse/\n- svc/src/**: the service\n\nReturn the fixed shape."}})
	files["transcripts/s1.jsonl"] = ""
	files["transcripts/s1/subagents/agent-"+scopeAgent+".jsonl"] = string(prompt) + "\n" + `{"type":"assistant","message":{"content":[]}}` + "\n"
	if base == "repo" || base == "ldd" || base == "folder" || base == "ws" {
		files["proj/src/app.py"] = "X = 1\n"
	}
	if base == "ldd" {
		files["proj/openspec/config.yaml"] = ""
	}
	if base == "ws" {
		files["proj/openspec/config.yaml"] = "workspace:\n  lib: ../lib\n"
		for _, r := range []string{"lib", "site", "shared"} {
			files[r+"/src/a.py"], files[r+"/.env"] = "X = 1\n", "KEY=v\n"
		}
		// Only this tree's settings name the shared repository.
		t.settings = filepath.Join(dir, "settings.toml")
		files["settings.toml"] = "[sandbox]\nmode = \"off\"\nallow_write = [\"" + filepath.Join(dir, "shared") + "\"]\n"
	}
	if err := t.write(files); err != nil {
		return nil, err
	}
	for _, sub := range []string{"proj/src", "other", "tmpdir", "state-old", "state-new"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	current := filepath.Join(t.home, ".local", "share", "jhon", "current")
	links := [][2]string{{filepath.Join(filepath.Dir(current), "releases", strings.Repeat("a", 40)), current},
		{filepath.Join(current, "build", "jhon"), filepath.Join(t.home, ".local", "bin", "jhon")},
		{filepath.Join(current, "commands", "jhon-ldd.md"), filepath.Join(t.home, ".claude", "commands", "jhon-ldd.md")}}
	for _, l := range links {
		if err := first(os.MkdirAll(filepath.Dir(l[1]), 0o755), os.Symlink(l[0], l[1])); err != nil {
			return nil, err
		}
	}
	gits := [][]string{{"other", "init", "-q", "-b", "main"}}
	switch base {
	case "corpus":
		gits = append(gits, []string{"proj", "init", "-q", "-b", "main"})
	case "repo", "ldd":
		gits = append(gits, []string{"proj", "init", "-q", "-b", "main"}, []string{"proj", "add", "-A"}, []string{"proj", "commit", "-q", "-m", "base"})
	case "ws":
		gits = append(gits, []string{"proj", "init", "-q", "-b", "main"}, []string{"proj", "add", "-A"}, []string{"proj", "commit", "-q", "-m", "base"},
			[]string{"proj", "remote", "add", "origin", "git@github.com:acme/app.git"},
			[]string{"proj", "worktree", "add", "-q", "-b", "w", filepath.Join(dir, "proj-worktrees", "wt")},
			[]string{"lib", "init", "-q", "-b", "main"}, []string{"site", "init", "-q", "-b", "main"}, []string{"shared", "init", "-q", "-b", "main"})
	}
	for _, g := range gits {
		c := exec.Command("git", append([]string{"-C", filepath.Join(dir, g[0]), "-c", "user.name=guarddiff", "-c", "user.email=guarddiff@invalid", "-c", "commit.gpgsign=false"}, g[1:]...)...)
		c.Env = t.env(&binary{name: "old"})
		if out, err := c.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git %s: %v: %s", strings.Join(g[1:], " "), err, out)
		}
	}
	// A change that alters behaviour, its record written, no RED before GREEN.
	if base == "repo" || base == "ldd" {
		change := map[string]string{"proj/src/app.py": "X = 2\n", "proj/tests/test_app.py": "def test_x(): pass\n"}
		if base == "ldd" {
			change["proj/openspec/changes/c1/tasks.md"] = "## Intent\nDelivers: X is 2\n"
		}
		if err := t.write(change); err != nil {
			return nil, err
		}
	}
	// The owner's joins, in each binary's own state.
	for _, b := range d.bins {
		for _, r := range map[bool][]string{true: {"lib", "site"}}[base == "ws"] {
			c := exec.Command(b.path, "workspace", "join", t.proj)
			c.Dir, c.Env = filepath.Join(dir, r), t.env(b)
			if out, err := c.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("%s workspace join in %s: %v: %s", b.path, r, err, out)
			}
		}
	}
	for _, b := range d.bins {
		if mode == "" {
			continue
		}
		c := exec.Command(b.path, "mode", mode)
		c.Dir, c.Env = t.proj, t.env(b)
		if out, err := c.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%s mode %s in %s: %v: %s", b.path, mode, place, err, out)
		}
	}
	return t, nil
}

func first(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (t *tree) write(files map[string]string) error {
	for rel, body := range files {
		p := filepath.Join(t.dir, rel)
		if err := first(os.MkdirAll(filepath.Dir(p), 0o755), os.WriteFile(p, []byte(body), 0o644)); err != nil {
			return err
		}
	}
	return nil
}

// env is a binary's environment in t: nothing of the caller's but PATH.
func (t *tree) env(b *binary) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.home, "XDG_STATE_HOME=" + filepath.Join(t.dir, "state-"+b.name),
		"XDG_CONFIG_HOME=" + filepath.Join(t.dir, "config"), "XDG_CACHE_HOME=" + filepath.Join(t.dir, "cache"),
		"TMPDIR=" + filepath.Join(t.dir, "tmpdir"), "JHON_ROOT=" + b.root, "JHON_SETTINGS=" + t.settings, "JHON_TELEMETRY=off",
		"JHON_ENGRAM=/dev/null/engram", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
}

func (t *tree) expand(s string) string {
	return strings.NewReplacer("<SCRATCH>", t.dir, "<P>", t.proj, "<OTHER>", filepath.Join(t.dir, "other"),
		"<LIB>", filepath.Join(t.dir, "lib"), "<SITE>", filepath.Join(t.dir, "site"), "<SHARED>", filepath.Join(t.dir, "shared"), "<WT>", filepath.Join(t.dir, "proj-worktrees", "wt"),
		"<TMP>", filepath.Join(t.dir, "tmpdir"), "<HOME>", t.home, "<FW>", t.fw).Replace(s)
}

// args is the hook call judging l by guards: pre-tool, or route-guard alone.
func (t *tree) args(b *binary, gs ...judged) []string {
	if len(gs) == 1 && gs[0].guard == "route-guard" {
		return append([]string{"hook", "route-guard", "--agent", "*"}, t.guardArgs("route-guard", b)...)
	}
	args := []string{"hook", "pre-tool", "--agent", "*"}
	for _, g := range gs {
		args = append(append(args, g.guard), t.guardArgs(g.guard, b)...)
	}
	return args
}

// guardArgs are g's arguments as the Claude Code render gives them
// (render/claude.go hooksBlock): route-guard's are the bounded writers.
func (t *tree) guardArgs(g string, b *binary) []string {
	switch g {
	case "inspect-guard":
		return b.inspect
	case "route-guard":
		return []string{"worker"}
	case "vcs-guard":
		return b.vcsDirs
	case "framework-guard":
		local := filepath.Join(t.home, ".local")
		return []string{t.fw, filepath.Join(local, "share", "jhon", "releases"), filepath.Join(local, "share", "jhon", "current"),
			filepath.Join(local, "bin", "jhon"), filepath.Join(t.home, ".claude", "CLAUDE.md"), filepath.Join(t.home, ".claude", "agents", "general.md"),
			filepath.Join(t.home, ".agents", "skills", "ldd"), filepath.Join(t.home, ".claude", "commands", "jhon-ldd.md")}
	}
	return nil
}

// payload is the PreToolUse payload Claude Code sends for l.
func (t *tree) payload(l line) ([]byte, error) {
	input := t.expand(l.input)
	var toolInput any = map[string]any{"command": input}
	if l.tool != "Bash" {
		if err := json.Unmarshal([]byte(input), &toolInput); err != nil {
			return nil, fmt.Errorf("the %s input is not JSON: %v", l.tool, err)
		}
	}
	cwd := t.proj
	if l.cwd != "" {
		cwd = t.expand(l.cwd)
	}
	p := map[string]any{"hook_event_name": "PreToolUse", "tool_name": l.tool, "tool_input": toolInput, "cwd": cwd, "session_id": "s", "permission_mode": "default"}
	if l.caller != "top-default" {
		p["permission_mode"] = "bypassPermissions"
	}
	if l.caller == "subagent" {
		p["agent_id"], p["agent_type"], p["transcript_path"] = scopeAgent, "worker", filepath.Join(t.dir, "transcripts", "s1.jsonl")
	}
	if l.caller == inspector {
		p["agent_id"], p["agent_type"] = "ainspector0123456", "prism-safety"
	}
	return json.Marshal(p)
}
