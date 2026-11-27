package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"
	"mvdan.cc/sh/v3/syntax"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// The launch check (T266, odd/tasks/v0-1-release.md, the owner's decision
// in L1742; T265 folded in). A sandboxed command can write a file that an
// agent runtime runs outside the sandbox later: a hook in a sibling
// worktree's .claude/settings.local.json, a pi extension in another
// project inside sandbox.allow_write, a `.mcp.json` where there was none.
// Mounts cannot close that without costing ordinary work, so `jhon start`
// checks before it launches: what runs outside the sandbox in each root
// the runtime reads (the working folder and its repository's top) is
// compared with what the owner last approved for that root, a record under
// jhon's state folder (<state>/jhon/hook-approvals/<sha256 of the
// root>.json), which the sandbox keeps read-only and the guards keep from
// every agent (grant.InGrants). When it differs the start shows what
// changed and asks; with no terminal to ask on it starts nothing and names
// `--approve-hooks`.
//
// Only what runs counts, so the question stays rare (the owner's 30 days:
// 10 commits touched .claude/ in one project, every one of them rules,
// skills' prose or an agent's prose, none a hook, a server or a setting
// that names a command):
//   - .claude/settings.json and settings.local.json without the keys that
//     run nothing and that Claude Code itself writes in ordinary work
//     (inertClaudeKeys: permissions, the .mcp.json servers enabled);
//   - .claude/hooks/**, .pi/extensions/** and .pi/skills/**, every file;
//   - .claude/skills, agents and commands: only a markdown file's
//     frontmatter hooks and mcpServers, read as YAML, and its lines that
//     run a command (!`…`);
//   - .mcp.json, .pi/mcp.json and .pi/settings.json, all of them;
//   - a file (a folder too, for pi's extensions, skills and packages) that
//     one of those settings names, read as the shell reads a command
//     (`"$CLAUDE_PROJECT_DIR"/tools/guard.sh`), inside the root or, when
//     the owner can write it, anywhere.
//
// A root never approved records without asking only when nothing runs
// there (L1768: a commit is no approval, as a sandboxed command can
// commit); anything else asks once, "(first start here)". `jhon sync`,
// from the owner's terminal, seeds the roots he never started in
// (seedHooks).

// approveHooksFlag approves what changed without asking, then starts.
const approveHooksFlag = "--approve-hooks"

// inertClaudeKeys are the top-level keys of Claude Code's settings that
// run nothing outside the sandbox: permissions (the guards and the sandbox
// still judge every call), the model and output style, and the switches of
// the .mcp.json servers, whose commands the check compares itself. Claude
// Code writes several of them into settings.local.json in ordinary work.
var inertClaudeKeys = []string{"$schema", "permissions", "enabledMcpjsonServers", "disabledMcpjsonServers",
	"enableAllProjectMcpServers", "model", "outputStyle", "includeCoAuthoredBy", "cleanupPeriodDays",
	"spinnerTipsEnabled", "alwaysThinkingEnabled"}

// A hookFile is one counted file of a root: its path from the root (an
// absolute path outside it), the sha256 of what runs in it, and the lines
// the question shows (secrets hidden).
type hookFile struct {
	Path  string   `json:"path"`
	Sum   string   `json:"sha256"`
	Shown []string `json:"shown"`
	// unmasked is a file whose Shown lines are still as read: a start whose
	// files equal the record never shows them, so masked only when shown or
	// recorded (masked).
	unmasked bool
}

// masked is files with every Shown line through maskSecrets.
func masked(files []hookFile) []hookFile {
	for i := range files {
		if files[i].unmasked {
			shown := make([]string, len(files[i].Shown))
			for j, l := range files[i].Shown {
				shown[j] = maskSecrets(l)
			}
			files[i].Shown, files[i].unmasked = shown, false
		}
	}
	return files
}

// A hookRecord is what the owner approved for a root.
type hookRecord struct {
	Root       string     `json:"root"`
	ApprovedAt string     `json:"approved_at"`
	Files      []hookFile `json:"files"`
}

// hookLimits bound what one root's check reads, so a start stays cheap
// whatever a sandboxed command plants: a file's bytes, the bytes of every
// file read together, and the files counted one by one. A file past a byte
// limit is compared by its size, times and inode (the kernel sets its
// ctime, which no user can set back); the files past the count limit are
// folded into one entry, compared the same way.
const (
	hookFileMax  = 1 << 20
	hookTotalMax = 16 << 20
	hookFilesMax = 1000
	shownMax     = 40
)

// pastLimit is the entry the files past hookFilesMax fold into.
var pastLimit = fmt.Sprintf("(files past the first %d)", hookFilesMax)

// hooksApproved is the launch check for runtime (named as the question
// names it), whose own start command is cmd: whether the start may go on,
// and its exit code when not.
func hooksApproved(runtime, cmd string, approve bool) (int, bool) {
	type pending struct {
		root  string
		rec   *hookRecord
		files []hookFile
	}
	var asks []pending
	var silent []hookRecord
	for _, root := range hookRoots() {
		files := hookSignature(root)
		rec, _ := readHookRecord(root)
		switch {
		case rec != nil && sameHooks(rec.Files, files):
		case rec == nil && len(files) == 0:
			silent = append(silent, newHookRecord(root, files))
		default:
			asks = append(asks, pending{root, rec, masked(files)})
		}
	}
	for _, r := range silent {
		_ = writeHookRecord(r)
	}
	if len(asks) == 0 {
		return 0, true
	}
	o := out()
	var b strings.Builder
	b.WriteString(o.R + "jhon:" + o.Z + " what runs outside the sandbox here changed since you last approved it:\n")
	for _, p := range asks {
		var before []hookFile
		if p.rec != nil {
			before = p.rec.Files
			fmt.Fprintf(&b, "  %s\n", p.root)
		} else {
			fmt.Fprintf(&b, "  %s (first start here)\n", p.root)
		}
		b.WriteString(hookChanges(before, p.files))
	}
	fmt.Fprint(o.E, b.String())
	approveAll := func() (int, bool) {
		for _, p := range asks {
			if err := writeHookRecord(newHookRecord(p.root, p.files)); err != nil {
				fmt.Fprintf(o.E, "%sjhon:%s %s not started: the approval cannot be recorded: %v\n", o.R, o.Z, runtime, err)
				return 1, false
			}
		}
		return 0, true
	}
	switch {
	case approve:
		code, ok := approveAll()
		if ok {
			fmt.Fprintf(o.E, "%sjhon:%s approved with %s\n", o.R, o.Z, approveHooksFlag)
		}
		return code, ok
	case !isTerminal(os.Stdin) || sandbox.Inside():
		fmt.Fprintf(o.E, "%sjhon:%s %s not started: no terminal to ask on; to approve these and start, run `%s %s`\n", o.R, o.Z, runtime, cmd, approveHooksFlag)
		return 1, false
	}
	fmt.Fprintf(o.E, "%sjhon:%s approve these and start %s? [y/N] ", o.R, o.Z, runtime)
	if !yes(readAnswer(os.Stdin)) {
		fmt.Fprintf(o.E, "%sjhon:%s %s not started; nothing was approved\n", o.R, o.Z, runtime)
		return 1, false
	}
	return approveAll()
}

// yes is whether an answer is y or yes.
func yes(answer string) bool {
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}

// readAnswer is one line of f, read a byte at a time so nothing after it
// is taken from the runtime that starts next.
func readAnswer(f *os.File) string {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := f.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
		}
		if err != nil || len(line) > 64 {
			break
		}
	}
	return string(line)
}

// hookRoots are the roots the runtime reads: the working folder and the
// top of its repository, each once. The home is none: its .claude is the
// owner's own settings, which no project names (project.ReadRepo).
func hookRoots() []string {
	here := project.Realish(cwd())
	var roots []string
	if here != project.Realish(project.Home()) {
		roots = append(roots, here)
	}
	if top := project.ReadRepo(here).Top; top != "" && top != here {
		roots = append(roots, top)
	}
	return roots
}

// hookRecordDir is where the approvals live: jhon's state folder, which
// the sandbox keeps read-only.
func hookRecordDir() string { return filepath.Join(filepath.Dir(grant.Dir()), "hook-approvals") }

func hookRecordPath(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(hookRecordDir(), hex.EncodeToString(sum[:])+".json")
}

func readHookRecord(root string) (*hookRecord, error) {
	b, err := os.ReadFile(hookRecordPath(root))
	if err != nil {
		return nil, err
	}
	var r hookRecord
	if err := json.Unmarshal(b, &r); err != nil || r.Root != root {
		return nil, errors.New("not a record of this root")
	}
	return &r, nil
}

func newHookRecord(root string, files []hookFile) hookRecord {
	r := hookRecord{Root: root, ApprovedAt: isodate.Format(time.Now()), Files: files}
	if r.Files == nil {
		r.Files = []hookFile{}
	}
	return r
}

// writeHookRecord replaces root's record, written beside it and renamed.
func writeHookRecord(r hookRecord) error {
	dir := hookRecordDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), hookRecordPath(r.Root)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// sameHooks is whether a and b count the same files with the same sums.
func sameHooks(a, b []hookFile) bool {
	return slices.EqualFunc(a, b, func(x, y hookFile) bool { return x.Path == y.Path && x.Sum == y.Sum })
}

// hookSignature is what runs outside the sandbox in root, its files sorted
// by path.
func hookSignature(root string) []hookFile {
	s := &signer{root: root, home: project.Home(), seen: map[string]bool{}, past: sha256.New()}
	for _, name := range []string{".claude/settings.json", ".claude/settings.local.json"} {
		s.json(name, inertClaudeKeys, root, nil)
	}
	for _, name := range []string{".mcp.json", ".pi/mcp.json"} {
		s.json(name, nil, root, nil)
	}
	// pi resolves a project setting's resources from .pi; its prompts and
	// themes run nothing.
	s.json(".pi/settings.json", nil, filepath.Join(root, ".pi"), piResource)
	for _, dir := range []string{".claude/hooks", ".pi/extensions", ".pi/skills"} {
		s.tree(dir, func(string) bool { return true }, s.whole)
	}
	for _, dir := range []string{".claude/skills", ".claude/agents", ".claude/commands"} {
		s.tree(dir, func(p string) bool { return strings.HasSuffix(p, ".md") }, s.markdown)
	}
	for _, r := range s.refs {
		if r.dir {
			s.tree(r.name, func(string) bool { return true }, s.whole)
		} else {
			s.count(r.name, nil, s.whole)
		}
	}
	if s.pastN > 0 {
		s.files = append(s.files, hookFile{Path: pastLimit, Sum: hex.EncodeToString(s.past.Sum(nil)),
			Shown: []string{fmt.Sprintf("%d more files, not read: compared by name, size, times and inode", s.pastN)}})
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].Path < s.files[j].Path })
	return s.files
}

// piResource is how .pi/settings.json's leaf at path names what runs: a
// resource of extensions, skills or packages names a file or a folder,
// with or without a slash; prompts and themes name nothing that runs.
func piResource(path string) (use, resource bool) {
	top, _, _ := strings.Cut(strings.TrimPrefix(path, "."), "[")
	top, _, _ = strings.Cut(top, ".")
	switch top {
	case "prompts", "themes":
		return false, false
	case "extensions", "skills", "packages":
		return true, true
	}
	return true, false
}

// A signer gathers a root's counted files. A name is a path from the root,
// or an absolute path outside it.
type signer struct {
	root  string
	home  string
	files []hookFile
	seen  map[string]bool
	refs  []hookRef
	n     int
	bytes int64
	past  hash.Hash
	pastN int
}

// A hookRef is a file or a folder a setting names.
type hookRef struct {
	name string
	dir  bool
}

func (s *signer) abs(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(s.root, filepath.FromSlash(name))
}

// count counts the file name with what (fi is its stat when the caller has
// one): a FIFO or another file that is not regular is named, never opened;
// one past hookFilesMax is folded into pastLimit; one past a byte limit is
// compared by its identity; any other is read and handed to what.
func (s *signer) count(name string, fi os.FileInfo, what func(name string, b []byte)) {
	if s.seen[name] {
		return
	}
	path := s.abs(name)
	if fi == nil {
		var err error
		if fi, err = os.Stat(path); err != nil {
			return
		}
	}
	s.seen[name] = true
	s.n++
	if s.n > hookFilesMax {
		s.pastN++
		io.WriteString(s.past, name+"\x00"+fileIdentity(fi)+"\n")
		return
	}
	if !fi.Mode().IsRegular() {
		s.add(name, []string{"(not a regular file)"}, []string{"(not a regular file)"})
		return
	}
	if fi.Size() > hookFileMax || s.bytes+fi.Size() > hookTotalMax {
		s.identity(name, fi)
		return
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		note := "(cannot be read: " + err.Error() + ")"
		s.add(name, []string{note}, []string{note})
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		s.add(name, []string{"(not a regular file)"}, []string{"(not a regular file)"})
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, hookFileMax+1))
	if err != nil {
		note := "(cannot be read: " + err.Error() + ")"
		s.add(name, []string{note}, []string{note})
		return
	}
	if len(b) > hookFileMax {
		s.identity(name, fi)
		return
	}
	s.bytes += int64(len(b))
	what(name, b)
}

// identity counts a file too big to read by its size, times and inode.
func (s *signer) identity(name string, fi os.FileInfo) {
	shown := fmt.Sprintf("(%d bytes, modified %s, not read: compared by size, times and inode)", fi.Size(), fi.ModTime().UTC().Format(time.RFC3339))
	s.add(name, []string{fileIdentity(fi)}, []string{shown})
}

func (s *signer) add(name string, real, shown []string) {
	sum := sha256.Sum256([]byte(strings.Join(real, "\n")))
	s.files = append(s.files, hookFile{Path: name, Sum: hex.EncodeToString(sum[:]), Shown: shown})
}

// whole counts every line of a file.
func (s *signer) whole(name string, b []byte) {
	if bytes.IndexByte(b, 0) >= 0 {
		sum := sha256.Sum256(b)
		s.add(name, []string{hex.EncodeToString(sum[:])}, []string{fmt.Sprintf("(binary, %d bytes)", len(b))})
		return
	}
	s.add(name, []string{string(b)}, textLines(b))
	s.files[len(s.files)-1].unmasked = true
}

// runnerKeys are the frontmatter keys of a skill, an agent or a command
// that run something.
var runnerKeys = []string{"hooks", "mcpServers"}

// markdown counts a skill's, agent's or command's lines that run
// something: its frontmatter hooks and mcpServers, and !`…` lines.
func (s *signer) markdown(name string, b []byte) {
	var real, runs []string
	lines := textLines(b)
	body := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := len(lines)
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end, body = i, i+1
				break
			}
		}
		if body == 0 {
			body = len(lines)
		}
		real, runs = frontmatterRuns(lines[1:end])
	}
	for _, l := range lines[body:] {
		if strings.Contains(l, "!`") || strings.HasPrefix(strings.TrimSpace(l), "```!") {
			runs = append(runs, l)
			real = append(real, l)
		}
	}
	if len(runs) == 0 {
		return
	}
	s.add(name, real, runs)
	s.files[len(s.files)-1].unmasked = true
}

// frontmatterRuns is what runs in a frontmatter, read as YAML reads it, so
// no spelling of a key hides it (`"hooks":`, `hooks :`, an indented root
// mapping, a merge key): the lines of its hooks and mcpServers keys (real
// also holds their values as decoded, so an anchor they name counts). A
// frontmatter that is no YAML mapping counts whole when it mentions one.
func frontmatterRuns(fm []string) (real, shown []string) {
	text := strings.Join(fm, "\n")
	whole := func() ([]string, []string) {
		l := strings.ToLower(text)
		if strings.Contains(l, "hooks") || strings.Contains(l, "mcpservers") || strings.Contains(text, `\`) {
			return fm, fm
		}
		return nil, nil
	}
	var doc yaml.Node
	if yaml.Unmarshal([]byte(text), &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return whole()
	}
	root := doc.Content[0]
	var decoded map[string]any
	if root.Decode(&decoded) != nil {
		return whole()
	}
	taken := map[int]bool{}
	direct := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if !slices.Contains(runnerKeys, k.Value) {
			continue
		}
		direct[k.Value] = true
		last := len(fm)
		if i+2 < len(root.Content) {
			last = max(root.Content[i+2].Line-1, k.Line)
		}
		for l := k.Line; l <= last && l <= len(fm); l++ {
			if !taken[l] {
				taken[l] = true
				shown = append(shown, fm[l-1])
			}
		}
	}
	for _, k := range runnerKeys {
		v, ok := decoded[k]
		if !ok {
			continue
		}
		if !direct[k] {
			// From a merge key: what runs is wherever its anchor is.
			return fm, fm
		}
		real = append(real, fmt.Sprintf("%s = %v", k, v))
	}
	if len(real) == 0 {
		return nil, nil
	}
	return append(real, shown...), shown
}

// tree counts each file under dir that keep takes, with what, following
// links to folders once each.
func (s *signer) tree(dir string, keep func(string) bool, what func(string, []byte)) {
	visited := map[string]bool{}
	var walk func(name string, fi os.FileInfo)
	walk = func(name string, fi os.FileInfo) {
		path := s.abs(name)
		var id string
		var err error
		if dev, ino, ok := devIno(fi); ok {
			id = fmt.Sprintf("%d:%d", dev, ino)
		} else {
			id, err = filepath.EvalSymlinks(path)
		}
		if err != nil || visited[id] {
			return
		}
		visited[id] = true
		entries, err := children(path)
		if err != nil {
			return
		}
		for _, cfi := range entries {
			child := name + "/" + cfi.Name()
			switch {
			case cfi.IsDir():
				walk(child, cfi)
			case keep(child) || !cfi.Mode().IsRegular():
				s.count(child, cfi, what)
			}
		}
	}
	if fi, err := os.Stat(s.abs(dir)); err == nil && fi.IsDir() {
		walk(dir, fi)
	}
}

// json counts a JSON file's leaves but those under the inert top-level
// keys; the files its strings name, read from base, are counted too
// (reference). filter, when set, says which leaves name something that
// runs, and which name a resource.
func (s *signer) json(name string, inert []string, base string, filter func(path string) (use, resource bool)) {
	s.count(name, nil, func(name string, b []byte) {
		var v any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if err := d.Decode(&v); err != nil {
			sum := sha256.Sum256(b)
			s.add(name, []string{hex.EncodeToString(sum[:])}, []string{fmt.Sprintf("(not valid JSON, %d bytes)", len(b))})
			return
		}
		if m, ok := v.(map[string]any); ok {
			for _, k := range inert {
				delete(m, k)
			}
			if len(m) == 0 {
				return
			}
		}
		var real, shown []string
		flatten(v, "", false, &real, &shown, func(path, value string) {
			use, resource := true, false
			if filter != nil {
				use, resource = filter(path)
			}
			if use {
				s.reference(value, base, resource)
			}
		})
		if len(real) == 0 {
			return
		}
		s.add(name, real, shown)
	})
}

// reference counts what a setting's string names: each word of it as the
// shell reads a command, $CLAUDE_PROJECT_DIR and $HOME expanded (inside
// single quotes too, so no quoting hides a path), and the value whole. A
// word counts when it holds a slash (any word of a resource), names a file
// (a folder too, for a resource) and is inside the root, or, outside it,
// the owner can write it.
func (s *signer) reference(value, base string, resource bool) {
	vars := map[string]string{"CLAUDE_PROJECT_DIR": s.root, "HOME": s.home}
	words := append([]string{value}, commandWords(value, vars)...)
	for _, w := range words {
		cands := []string{w}
		if _, after, ok := strings.Cut(w, "="); ok {
			cands = append(cands, after)
		}
		for _, c := range cands {
			if resource {
				c = strings.TrimLeft(c, "+-")
				if strings.HasPrefix(c, "!") || strings.Contains(c, ":") && !strings.HasPrefix(c, "/") && !strings.HasPrefix(c, ".") {
					continue
				}
			}
			if c == "" || (!resource && !strings.Contains(c, "/")) {
				continue
			}
			if rest, ok := strings.CutPrefix(c, "~/"); ok {
				c = filepath.Join(s.home, rest)
			}
			if strings.HasPrefix(c, "~") || strings.Contains(c, "$") {
				continue
			}
			path := c
			if !filepath.IsAbs(path) {
				path = filepath.Join(base, path)
			}
			s.refer(filepath.Clean(path), resource)
		}
	}
}

// refer adds the file (or a resource's folder) at path to what counts.
func (s *signer) refer(path string, resource bool) {
	fi, err := os.Stat(path)
	if err != nil || !(fi.Mode().IsRegular() || resource && fi.IsDir()) {
		return
	}
	name := path
	if rel, err := filepath.Rel(s.root, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
		name = filepath.ToSlash(rel)
	} else if rel == "." || !writable(path) {
		return
	}
	r := hookRef{name, fi.IsDir()}
	if !slices.Contains(s.refs, r) {
		s.refs = append(s.refs, r)
	}
}

// commandWords are the words of cmd as the shell reads it, quotes removed:
// a word with an expansion other than vars is none.
func commandWords(cmd string, vars map[string]string) []string {
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		var out []string
		for _, w := range strings.Fields(cmd) {
			out = append(out, expandVars(strings.NewReplacer(`"`, "", `'`, "").Replace(w), vars))
		}
		return out
	}
	var out []string
	syntax.Walk(f, func(n syntax.Node) bool {
		if w, ok := n.(*syntax.Word); ok {
			if s, ok := wordText(w.Parts, vars); ok {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// wordText is a word's text, quotes removed and vars expanded; ok is false
// for any other expansion.
func wordText(parts []syntax.WordPart, vars map[string]string) (string, bool) {
	var b strings.Builder
	for _, p := range parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(unescape(x.Value))
		case *syntax.SglQuoted:
			b.WriteString(expandVars(x.Value, vars))
		case *syntax.DblQuoted:
			s, ok := wordText(x.Parts, vars)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		case *syntax.ParamExp:
			var buf bytes.Buffer
			if syntax.NewPrinter().Print(&buf, x) != nil {
				return "", false
			}
			name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(buf.String(), "$"), "{"), "}")
			v, ok := vars[name]
			if !ok || (buf.String() != "$"+name && buf.String() != "${"+name+"}") {
				return "", false
			}
			b.WriteString(v)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func expandVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "${"+k+"}", v), "$"+k, v)
	}
	return s
}

var plainKey = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)

// secretKey is a key whose value the question never shows; env and
// headers hide every value under them.
var secretKey = regexp.MustCompile(`(?i)(token|secret|passw(or)?d|api[_-]?key|authorization|bearer|credential|cookie)`)

// flatten writes v's leaves as `path = value` lines, the keys sorted: real
// with every value, shown with the hidden ones as `…` and the rest through
// maskSecrets; leaf sees every string leaf. In an array, the value after a
// flag that names a secret (`--api-key`, `-u`) is hidden.
func flatten(v any, path string, hidden bool, real, shown *[]string, leaf func(path, value string)) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			*real, *shown = append(*real, path+" = {}"), append(*shown, path+" = {}")
			return
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if !plainKey.MatchString(k) {
				q, _ := json.Marshal(k)
				p = "[" + string(q) + "]"
			} else if path != "" {
				p = "." + k
			}
			h := hidden || strings.EqualFold(k, "env") || strings.EqualFold(k, "headers") || secretKey.MatchString(k)
			flatten(x[k], path+p, h, real, shown, leaf)
		}
	case []any:
		if len(x) == 0 {
			*real, *shown = append(*real, path+" = []"), append(*shown, path+" = []")
			return
		}
		next := false
		for i, e := range x {
			flatten(e, path+"["+strconv.Itoa(i)+"]", hidden || next, real, shown, leaf)
			s, ok := e.(string)
			next = ok && secretFlag(s)
		}
	default:
		encode := func(x any) string {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(x)
			return strings.TrimSuffix(buf.String(), "\n")
		}
		*real = append(*real, path+" = "+encode(x))
		s, isString := x.(string)
		if isString {
			leaf(path, s)
		}
		switch {
		case hidden:
			*shown = append(*shown, path+" = …")
		case isString:
			*shown = append(*shown, path+" = "+encode(maskSecrets(s)))
		default:
			*shown = append(*shown, path+" = "+encode(x))
		}
	}
}

// secretName is whether a flag's or a variable's name names a secret: one
// of its words (split at - and _) is key, token, pat, password, secret, …,
// or holds token, secret or passw (GH_PAT, GITHUB_TOKEN, API_KEY,
// AWS_SECRET_ACCESS_KEY, PGPASSWORD, --api-key, --client-secret).
func secretName(name string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' }) {
		switch {
		case slices.Contains([]string{"key", "apikey", "pat", "pass", "auth", "bearer", "credential", "credentials", "cookie"}, w),
			strings.Contains(w, "token"), strings.Contains(w, "secret"), strings.Contains(w, "passw"):
			return true
		}
	}
	return false
}

// secretFlag is whether an argument is a flag whose next argument is a
// secret: a flag that names one, or curl's -u/--user.
func secretFlag(a string) bool {
	if a == "-u" || a == "--user" {
		return true
	}
	name, ok := strings.CutPrefix(a, "-")
	return ok && !strings.Contains(name, "=") && secretName(strings.TrimPrefix(name, "-"))
}

var (
	urlAny        = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)
	urlUserinfo   = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@?#]+@`)
	urlQueryValue = regexp.MustCompile(`([?&;][^=&#;\s]+=)[^&#;\s]*`)
	authorization = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*)[^"'\n]+`)
	authScheme    = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)[^\s"']+`)
	curlUser      = regexp.MustCompile(`(^|\s)(-u|--user)(\s+|=)?([^\s:"']*):("[^"]*"|'[^']*'|[^\s"']+)`)
	flagValue     = regexp.MustCompile(`(^|[\s"'])(--?[A-Za-z][A-Za-z0-9_-]*)(=|\s+)("[^"]*"|'[^']*'|[^\s"']+)`)
	assignment    = regexp.MustCompile(`(^|[\s"';&|(])([A-Za-z_][A-Za-z0-9_]*)=("[^"]*"|'[^']*'|[^\s"';&|)]+)`)
	secretValue   = regexp.MustCompile(`(?i)((?:token|secret|passw(?:or)?d|api[_-]?key|authorization|credential|cookie)[A-Za-z0-9_]*["']?\s*[:=]\s*)[^\s"']+`)
)

// maskSecrets is a value or a line as the question shows it: in a url its
// userinfo and every query value, an Authorization header, a Bearer or
// Basic credential, curl's -u password, the value of a flag (`--token x`,
// `--api-key=x`) or a variable (`GH_PAT=x`) whose name names a secret, and
// a value set to a secret-named key, all `…`.
func maskSecrets(s string) string {
	s = urlAny.ReplaceAllStringFunc(s, func(u string) string {
		return urlQueryValue.ReplaceAllString(urlUserinfo.ReplaceAllString(u, "${1}…@"), "${1}…")
	})
	s = authorization.ReplaceAllString(s, "${1}…")
	s = authScheme.ReplaceAllString(s, "${1}${2}…")
	s = curlUser.ReplaceAllString(s, "${1}${2}${3}${4}:…")
	s = replaceGroups(flagValue, s, func(g []string) string {
		if secretName(strings.TrimLeft(g[2], "-")) {
			return g[1] + g[2] + g[3] + "…"
		}
		return g[0]
	})
	s = replaceGroups(assignment, s, func(g []string) string {
		if secretName(g[2]) {
			return g[1] + g[2] + "=…"
		}
		return g[0]
	})
	return secretValue.ReplaceAllString(s, "${1}…")
}

// replaceGroups replaces each match of re in s with f of its groups.
func replaceGroups(re *regexp.Regexp, s string, f func(groups []string) string) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		g := make([]string, len(m)/2)
		for i := range g {
			if m[2*i] >= 0 {
				g[i] = s[m[2*i]:m[2*i+1]]
			}
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(f(g))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// textLines are b's lines, without a last empty one.
func textLines(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// hookChanges is what the question shows for one root: each counted file
// new, changed or removed since before, with its lines added and removed.
func hookChanges(before, now []hookFile) string {
	old := map[string]hookFile{}
	for _, f := range before {
		old[f.Path] = f
	}
	var b strings.Builder
	paths := map[string]bool{}
	for _, f := range now {
		paths[f.Path] = true
	}
	for _, f := range before {
		paths[f.Path] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	cur := map[string]hookFile{}
	for _, f := range now {
		cur[f.Path] = f
	}
	for _, p := range sorted {
		o, had := old[p]
		n, has := cur[p]
		switch {
		case had && has && o.Sum == n.Sum:
			continue
		case !had:
			fmt.Fprintf(&b, "    new %s\n", p)
			b.WriteString(diffLines(nil, n.Shown))
		case !has:
			fmt.Fprintf(&b, "    removed %s\n", p)
		default:
			fmt.Fprintf(&b, "    changed %s\n", p)
			if d := diffLines(o.Shown, n.Shown); d != "" {
				b.WriteString(d)
			} else {
				b.WriteString("      (a value jhon does not show changed)\n")
			}
		}
	}
	return b.String()
}

// diffLines is the lines of a removed (-) and of b added (+), as a longest
// common subsequence leaves them, at most shownMax shown.
func diffLines(a, b []string) string {
	var out []string
	if len(a)*len(b) > 4_000_000 {
		for _, l := range a {
			out = append(out, "- "+l)
		}
		for _, l := range b {
			out = append(out, "+ "+l)
		}
	} else {
		lcs := make([][]int, len(a)+1)
		for i := range lcs {
			lcs[i] = make([]int, len(b)+1)
		}
		for i := len(a) - 1; i >= 0; i-- {
			for j := len(b) - 1; j >= 0; j-- {
				if a[i] == b[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(a) || j < len(b) {
			switch {
			case i < len(a) && j < len(b) && a[i] == b[j]:
				i, j = i+1, j+1
			case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
				out = append(out, "- "+a[i])
				i++
			default:
				out = append(out, "+ "+b[j])
				j++
			}
		}
	}
	var s strings.Builder
	for k, l := range out {
		if k == shownMax {
			fmt.Fprintf(&s, "      … %d more lines\n", len(out)-shownMax)
			break
		}
		s.WriteString("      " + l + "\n")
	}
	return s.String()
}
