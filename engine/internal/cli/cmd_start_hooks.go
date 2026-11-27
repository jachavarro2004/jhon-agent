package cli

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
//     frontmatter hooks and mcpServers, and its lines that run a command
//     (!`…`);
//   - .mcp.json, .pi/mcp.json and .pi/settings.json, all of them;
//   - a file inside the root that a hook's command, or an MCP server's
//     command, names (`"$CLAUDE_PROJECT_DIR/tools/guard.sh"`).
//
// A root never approved records without asking only when HEAD holds each
// counted file as it is on the disk (its blob, hashed here, so no index
// flag hides a change) and, where another worktree of the same repository
// was approved, it equals one of those: a sandboxed command can commit,
// so a commit is no approval in a repository the owner already reviewed.
// A root with nothing that runs records empty, so a file that appears
// later asks.

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

// A hookFile is one counted file of a root: its path from the root, the
// sha256 of what runs in it, and the lines the question shows (secrets
// hidden). raw is the file as read, for the blob check.
type hookFile struct {
	Path  string   `json:"path"`
	Sum   string   `json:"sha256"`
	Shown []string `json:"shown"`
	raw   []byte
}

// A hookRecord is what the owner approved for a root.
type hookRecord struct {
	Root       string     `json:"root"`
	Common     string     `json:"common,omitempty"`
	ApprovedAt string     `json:"approved_at"`
	Files      []hookFile `json:"files"`
}

// hookLimits bound what one root's check reads: a file's bytes, and every
// counted folder's files and bytes together. Past a limit a file counts by
// its size alone.
const (
	hookFileMax  = 1 << 20
	hookTotalMax = 64 << 20
	hookFilesMax = 20000
	shownMax     = 40
)

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
		case rec == nil && (len(files) == 0 || committedAsIs(root, files)):
			silent = append(silent, newHookRecord(root, files))
		default:
			asks = append(asks, pending{root, rec, files})
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
	if a := strings.ToLower(strings.TrimSpace(readAnswer(os.Stdin))); a != "y" && a != "yes" {
		fmt.Fprintf(o.E, "%sjhon:%s %s not started; nothing was approved\n", o.R, o.Z, runtime)
		return 1, false
	}
	return approveAll()
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
	r := hookRecord{Root: root, Common: project.ReadRepo(root).Common, ApprovedAt: isodate.Format(time.Now()), Files: files}
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

// committedAsIs is whether a root never approved may record files without
// asking: HEAD holds each as it is on the disk, and, where another
// worktree of the repository was approved, they equal one of those.
func committedAsIs(root string, files []hookFile) bool {
	repo := project.ReadRepo(root)
	if repo.Top == "" {
		return false
	}
	var rels []string
	for _, f := range files {
		if f.raw == nil {
			return false
		}
		rel, err := filepath.Rel(repo.Top, filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil || strings.HasPrefix(rel, "..") {
			return false
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	format, _ := project.ReadGit(repo.Top, "rev-parse", "--show-object-format")
	out, err := project.ReadGitCmd(repo.Top, append([]string{"ls-tree", "-z", "--full-tree", "HEAD", "--"}, rels...)...).Output()
	if err != nil {
		return false
	}
	blobs := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 && f[1] == "blob" && (f[0] == "100644" || f[0] == "100755") {
			blobs[path] = f[2]
		}
	}
	for i, f := range files {
		if blobs[rels[i]] != blobID(format, f.raw) {
			return false
		}
	}
	if repo.Common == "" {
		return true
	}
	entries, _ := os.ReadDir(hookRecordDir())
	siblings := 0
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(hookRecordDir(), e.Name()))
		var r hookRecord
		if err != nil || json.Unmarshal(b, &r) != nil || r.Common != repo.Common || r.Root == root {
			continue
		}
		siblings++
		if sameHooks(r.Files, files) {
			return true
		}
	}
	return siblings == 0
}

// blobID is git's object id of a blob holding b, in format (sha1 or sha256).
func blobID(format string, b []byte) string {
	head := "blob " + strconv.Itoa(len(b)) + "\x00"
	if format == "sha256" {
		s := sha256.Sum256(append([]byte(head), b...))
		return hex.EncodeToString(s[:])
	}
	s := sha1.Sum(append([]byte(head), b...))
	return hex.EncodeToString(s[:])
}

// hookSignature is what runs outside the sandbox in root, its files sorted
// by path.
func hookSignature(root string) []hookFile {
	s := &signer{root: root, seen: map[string]bool{}}
	for _, name := range []string{".claude/settings.json", ".claude/settings.local.json"} {
		s.json(name, inertClaudeKeys)
	}
	for _, name := range []string{".mcp.json", ".pi/mcp.json"} {
		s.json(name, nil)
	}
	s.json(".pi/settings.json", nil)
	for _, dir := range []string{".claude/hooks", ".pi/extensions", ".pi/skills"} {
		s.tree(dir, func(string) bool { return true }, s.whole)
	}
	for _, dir := range []string{".claude/skills", ".claude/agents", ".claude/commands"} {
		s.tree(dir, func(p string) bool { return strings.HasSuffix(p, ".md") }, s.markdown)
	}
	for _, ref := range s.refs {
		if !s.seen[ref] {
			s.whole(ref)
		}
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].Path < s.files[j].Path })
	return s.files
}

// A signer gathers a root's counted files.
type signer struct {
	root  string
	files []hookFile
	seen  map[string]bool
	refs  []string
	count int
	bytes int64
}

// read is the file at rel, read only when it is a regular file (a FIFO
// planted there is named, never opened for reading), up to hookFileMax:
// its bytes, or why it is not read.
func (s *signer) read(rel string) ([]byte, string, bool) {
	path := filepath.Join(s.root, filepath.FromSlash(rel))
	fi, err := os.Stat(path)
	if err != nil {
		return nil, "", false
	}
	if !fi.Mode().IsRegular() {
		return nil, "(not a regular file)", true
	}
	s.count++
	if fi.Size() > hookFileMax || s.bytes+fi.Size() > hookTotalMax || s.count > hookFilesMax {
		return nil, fmt.Sprintf("(%d bytes, compared by size)", fi.Size()), true
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, "(cannot be read: " + err.Error() + ")", true
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, "(not a regular file)", true
	}
	b, err := io.ReadAll(io.LimitReader(f, hookFileMax+1))
	if err != nil {
		return nil, "(cannot be read: " + err.Error() + ")", true
	}
	s.bytes += int64(len(b))
	return b, "", true
}

func (s *signer) add(rel string, real, shown []string, raw []byte) {
	s.seen[rel] = true
	sum := sha256.Sum256([]byte(strings.Join(real, "\n")))
	s.files = append(s.files, hookFile{Path: rel, Sum: hex.EncodeToString(sum[:]), Shown: shown, raw: raw})
}

// whole counts every line of the file at rel.
func (s *signer) whole(rel string) {
	b, note, ok := s.read(rel)
	if !ok {
		return
	}
	if b == nil {
		s.add(rel, []string{note}, []string{note}, nil)
		return
	}
	if bytes.IndexByte(b, 0) >= 0 {
		sum := sha256.Sum256(b)
		s.add(rel, []string{hex.EncodeToString(sum[:])}, []string{fmt.Sprintf("(binary, %d bytes)", len(b))}, b)
		return
	}
	lines := textLines(b)
	shown := make([]string, len(lines))
	for i, l := range lines {
		shown[i] = maskLine(l)
	}
	s.add(rel, []string{string(b)}, shown, b)
}

// markdown counts a skill's, agent's or command's lines that run
// something: its frontmatter hooks and mcpServers, and !`…` lines.
func (s *signer) markdown(rel string) {
	b, note, ok := s.read(rel)
	if !ok {
		return
	}
	if b == nil {
		s.add(rel, []string{note}, []string{note}, nil)
		return
	}
	var runs []string
	lines := textLines(b)
	body := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		in := false
		for i := 1; i < len(lines); i++ {
			l := lines[i]
			if strings.TrimSpace(l) == "---" {
				body = i + 1
				break
			}
			if l != "" && l[0] != ' ' && l[0] != '\t' {
				key, _, _ := strings.Cut(l, ":")
				in = key == "hooks" || key == "mcpServers"
			}
			if in {
				runs = append(runs, l)
			}
		}
	}
	for _, l := range lines[body:] {
		if strings.Contains(l, "!`") || strings.HasPrefix(strings.TrimSpace(l), "```!") {
			runs = append(runs, l)
		}
	}
	if len(runs) == 0 {
		return
	}
	shown := make([]string, len(runs))
	for i, l := range runs {
		shown[i] = maskLine(l)
	}
	s.add(rel, runs, shown, b)
}

// tree counts each file under dir that keep takes, with count, following
// links to folders once each.
func (s *signer) tree(dir string, keep func(string) bool, count func(string)) {
	visited := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		path := filepath.Join(s.root, filepath.FromSlash(rel))
		real, err := filepath.EvalSymlinks(path)
		if err != nil || visited[real] {
			return
		}
		visited[real] = true
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, e := range entries {
			child := rel + "/" + e.Name()
			fi, err := os.Stat(filepath.Join(path, e.Name()))
			switch {
			case err == nil && fi.IsDir():
				walk(child)
			case err == nil && keep(child) || err == nil && !fi.Mode().IsRegular():
				count(child)
			}
		}
	}
	if fi, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(dir))); err == nil && fi.IsDir() {
		walk(dir)
	}
}

// json counts a JSON file's leaves but those under the inert top-level
// keys; the files inside the root its commands name are counted too.
func (s *signer) json(rel string, inert []string) {
	b, note, ok := s.read(rel)
	if !ok {
		return
	}
	if b == nil {
		s.add(rel, []string{note}, []string{note}, nil)
		return
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		sum := sha256.Sum256(b)
		s.add(rel, []string{hex.EncodeToString(sum[:])}, []string{fmt.Sprintf("(not valid JSON, %d bytes)", len(b))}, b)
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
	flatten(v, "", false, &real, &shown, func(key, value string) {
		if key == "command" {
			s.reference(value)
		}
	})
	if len(real) == 0 {
		return
	}
	s.add(rel, real, shown, b)
}

// reference counts each file inside the root that a command names.
func (s *signer) reference(command string) {
	for _, tok := range strings.Fields(command) {
		tok = strings.Trim(tok, `"'`)
		tok = strings.NewReplacer("${CLAUDE_PROJECT_DIR}", s.root, "$CLAUDE_PROJECT_DIR", s.root).Replace(tok)
		if !strings.Contains(tok, "/") || strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "~") || strings.Contains(tok, "$") {
			continue
		}
		path := tok
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.root, path)
		}
		path = filepath.Clean(path)
		rel, err := filepath.Rel(s.root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			if r := filepath.ToSlash(rel); !slices.Contains(s.refs, r) {
				s.refs = append(s.refs, r)
			}
		}
	}
}

var plainKey = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)

// secretKey is a key whose value the question never shows; env and
// headers hide every value under them.
var secretKey = regexp.MustCompile(`(?i)(token|secret|passw(or)?d|api[_-]?key|authorization|bearer|credential|cookie)`)

// flatten writes v's leaves as `path = value` lines, the keys sorted: real
// with every value, shown with the hidden ones as `…`.
func flatten(v any, path string, hidden bool, real, shown *[]string, leaf func(key, value string)) {
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
			if s, ok := x[k].(string); ok {
				leaf(k, s)
			}
			flatten(x[k], path+p, h, real, shown, leaf)
		}
	case []any:
		if len(x) == 0 {
			*real, *shown = append(*real, path+" = []"), append(*shown, path+" = []")
			return
		}
		for i, e := range x {
			flatten(e, path+"["+strconv.Itoa(i)+"]", hidden, real, shown, leaf)
		}
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(x)
		value := strings.TrimSuffix(buf.String(), "\n")
		*real = append(*real, path+" = "+value)
		if hidden {
			*shown = append(*shown, path+" = …")
		} else {
			*shown = append(*shown, path+" = "+maskURL(value))
		}
	}
}

var (
	urlCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s:@"']+:[^/\s@"']+@`)
	secretValue    = regexp.MustCompile(`(?i)((?:token|secret|passw(?:or)?d|api[_-]?key|authorization|credential)[A-Za-z0-9_]*["']?\s*[:=]\s*)\S+|(\bbearer\s+)\S+`)
)

func maskURL(s string) string { return urlCredentials.ReplaceAllString(s, "${1}…@") }

// maskLine is a line as the question shows it: a url's password and a
// value set to a key that names a secret are `…`.
func maskLine(l string) string {
	return maskURL(secretValue.ReplaceAllString(l, "${1}${2}…"))
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
