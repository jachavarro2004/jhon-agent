package vcs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A Reading is a read-only look at the working state of a repository
// (T268, L1762): `jhon status`, `jhon deliver check` and the guards compare
// the working tree with a test run's through one, and it writes nothing into
// the repository. The blobs and trees its WorkingTree stores go to a private
// object directory of its own in the system's temporary folder, with the
// repository's objects as an alternate, so a tree id is the one WorkingTree
// gives and every tree of the repository resolves; Close removes it. Each
// git it runs stops with its context. Use it from one goroutine.
type Reading struct {
	ctx context.Context
	top string
	// dir is the private folder: objects/ and the temporary index.
	dir string
	env []string
}

// OpenReading is a Reading of root's repository; the error is a root in no
// git work tree, or a private folder that cannot be made.
func OpenReading(ctx context.Context, root string) (*Reading, error) {
	top := GitTopLevel(project.Resolve("", root))
	if top == "" {
		return nil, errors.New(root + " is not inside a git work tree")
	}
	objects := git(top, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if !objects.ok {
		return nil, errors.New("the object directory of " + top + " cannot be read")
	}
	sweepReadings(os.TempDir(), time.Now())
	dir, err := os.MkdirTemp("", readingPrefix)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(dir, "objects"), 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	r := &Reading{ctx: ctx, top: top, dir: dir, env: []string{
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(dir, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + alternate(strings.TrimSpace(objects.out)),
	}}
	// A guard that stops waiting for a reading at its deadline exits before
	// the reading's own Close: the folder goes when the context ends, its
	// git killed, so none is left behind in the temporary folder (T268).
	context.AfterFunc(ctx, r.Close)
	return r, nil
}

// readingPrefix names a reading's private folder in the temporary folder.
const readingPrefix = "jhon-reading-"

// staleReading is how old a reading's folder is when no reading can still
// be using it: one hashes within its budget in seconds.
const staleReading = 10 * time.Minute

// sweepReadings removes the folders of readings a killed process left in
// tmp (T268: a Ctrl-C, or the shell's timeout, skips Close): a folder, not
// a link, named readingPrefix, older than staleReading, that this user
// owns. Anything it cannot read or remove is left as it is.
func sweepReadings(tmp string, now time.Time) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), readingPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleReading || !ownedByMe(info) {
			continue
		}
		os.RemoveAll(filepath.Join(tmp, e.Name()))
	}
}

// alternate is dir as one entry of GIT_ALTERNATE_OBJECT_DIRECTORIES: quoted,
// as git reads a C-style string, when it holds the list separator or starts
// with a quote.
func alternate(dir string) string {
	if !strings.ContainsRune(dir, filepath.ListSeparator) && !strings.HasPrefix(dir, `"`) {
		return dir
	}
	return strconv.Quote(dir)
}

// git is gitEnv with the reading's objects, stopped with its context.
func (r *Reading) git(env []string, root string, args ...string) result {
	if err := r.ctx.Err(); err != nil {
		return result{spawn: err}
	}
	c := project.ReadGitCmd(root, args...)
	if c.Err != nil {
		return outcome(nil, c.Err)
	}
	cc := exec.CommandContext(r.ctx, c.Path, c.Args[1:]...)
	// Its objects are thrown away, so they are stored, not compressed:
	// zlib was most of the time (T268: 100 MB in 1.5 s stored, 4.2 s at
	// level 1). The id is of the content either way.
	cc.Dir, cc.Env = c.Dir, project.WithGitConfig(append(append(c.Env, r.env...), env...), [][2]string{{"core.looseCompression", "0"}})
	out, err := cc.Output()
	if ctxErr := r.ctx.Err(); err != nil && ctxErr != nil {
		return result{spawn: ctxErr}
	}
	return outcome(out, err)
}

// Candidate is CandidateOf through the reading.
func (r *Reading) Candidate(exempt map[string]bool) Candidate {
	return candidateOf(r.git, r.top, exempt)
}

// TreeDiff is TreeDiff through the reading: a tree its WorkingTree wrote
// resolves.
func (r *Reading) TreeDiff(a, b string) ([]string, bool) { return treeDiff(r.git, r.top, a, b) }

// WorkingTree is WorkingTree through the reading, within the untracked
// budget: an untracked tree over it is not hashed, and the error, a
// *LargeError, names what to ignore. Any other error is the context's, or
// a tree git could not build.
func (r *Reading) WorkingTree(exempt map[string]bool) (string, error) {
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	if large := r.overBudget(exempt); large != nil {
		return "", large
	}
	tree, ok := workingTree(r.git, r.top, filepath.Join(r.dir, "index"), exempt)
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("the working tree could not be written as a git tree")
	}
	return tree, nil
}

// Close removes the reading's private folder, and every object it wrote.
// It may be called more than once.
func (r *Reading) Close() { os.RemoveAll(r.dir) }

// A Budget is how much untracked data a reading hashes: at most Files
// files and Bytes bytes git does not ignore, a tool's own data never
// counted. Hashing is what a guard's time goes to: the owner's 2.4 GB
// CodeGraph index took `jhon status` 90 s (L1762).
type Budget struct{ Files, Bytes int64 }

// untrackedBudget is the budget every reading keeps, measured (T268): on
// the owner's machine, at nice 10, a reading stores 100 MB of new data in
// 1.5 s and 5000 files of 1 KB in 0.5 s, so this keeps one member's
// hashing within about 2 s of the guard's 6 s.
var untrackedBudget = Budget{Files: 10000, Bytes: 128 << 20}

// A LargeError is an untracked tree over the budget: Files files of Bytes
// bytes git does not ignore, the entries that hold them (`git status
// --untracked-files=normal`: a folder git does not track at all, with a
// trailing slash, or a file), the largest first.
type LargeError struct {
	Files, Bytes int64
	Entries      []string
	Budget       Budget
}

// Folders are the entries that are folders: the .gitignore lines that
// leave them out.
func (e *LargeError) Folders() []string {
	return slices.DeleteFunc(slices.Clone(e.Entries), func(s string) bool { return !strings.HasSuffix(s, "/") })
}

func (e *LargeError) Error() string {
	limit := fmt.Sprintf("%d files", e.Budget.Files)
	if e.Files <= e.Budget.Files {
		limit = Size(e.Budget.Bytes)
	}
	lines := e.Folders()
	if len(lines) == 0 {
		lines = e.Entries
	}
	return fmt.Sprintf("the working tree holds %d untracked files (%s) git does not ignore, over the %s a check hashes in its time: %s; when they are build or test output, add them to .gitignore (%s), else commit them",
		e.Files, Size(e.Bytes), limit, firstFive(e.Entries), firstFive(lines))
}

// firstFive is the first five of list, and how many more there are.
func firstFive(list []string) string {
	s := strings.Join(list[:min(5, len(list))], ", ")
	if len(list) > 5 {
		s += fmt.Sprintf(", and %d more", len(list)-5)
	}
	return s
}

// Size is n bytes for a reader: 7 B, 1.5 KB, 372.0 MB, 2.4 GB.
func Size(n int64) string {
	units := []string{"KB", "MB", "GB", "TB"}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, u := float64(n)/1024, 0
	for f >= 1024 && u < len(units)-1 {
		f, u = f/1024, u+1
	}
	return fmt.Sprintf("%.1f %s", f, units[u])
}

// overBudget is the untracked tree, but a tool's own data and exempt, when
// it is over untrackedBudget; nil within it, or when git cannot list it
// (the add then says what it can). Listing reads names and sizes, never
// bytes.
func (r *Reading) overBudget(exempt map[string]bool) *LargeError {
	all := r.git(nil, r.top, append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, toolData()...)...)
	if !all.ok {
		return nil
	}
	var untracked []string
	for _, p := range strings.Split(all.out, "\x00") {
		if p != "" && !exempt[p] {
			untracked = append(untracked, p)
		}
	}
	return overBudget(r.git, r.top, untracked, exempt)
}

// UntrackedOverBudget is untracked, files git does not track nor ignore in
// the work tree top (paths relative to it), when they are over the budget a
// reading hashes; nil within it. Risk and the route scan no more than a
// reading hashes (T268): `jhon risk` read the owner's 3366 untracked build
// files whole. It reads names and sizes, never bytes.
func UntrackedOverBudget(top string, untracked []string) *LargeError {
	return overBudget(gitEnv, top, untracked, nil)
}

func overBudget(run runner, top string, untracked []string, exempt map[string]bool) *LargeError {
	sizes := map[string]int64{}
	var files, bytes int64
	for _, p := range untracked {
		files++
		if st, err := os.Lstat(filepath.Join(top, filepath.FromSlash(p))); err == nil && st.Mode().IsRegular() {
			sizes[p] = st.Size()
			bytes += st.Size()
		}
	}
	if files <= untrackedBudget.Files && bytes <= untrackedBudget.Bytes {
		return nil
	}
	e := &LargeError{Files: files, Bytes: bytes, Budget: untrackedBudget}
	normal := run(nil, top, append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=normal", "--"}, toolData()...)...)
	held := map[string]int64{}
	for _, f := range strings.Split(normal.out, "\x00") {
		if strings.HasPrefix(f, "?? ") && !exempt[f[3:]] {
			e.Entries = append(e.Entries, f[3:])
		}
	}
	for p, n := range sizes {
		for _, entry := range e.Entries {
			if p == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(p, entry) {
				held[entry] += n
				break
			}
		}
	}
	slices.SortFunc(e.Entries, func(a, b string) int {
		return cmp.Or(cmp.Compare(held[b], held[a]), cmp.Compare(a, b))
	})
	return e
}
