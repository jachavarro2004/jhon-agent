package execution

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// FirstRunMinutes is the deadline of a run that has to end once for
// pytest-testmon's record to narrow at all, when the run names none
// (--timeout): a first run (Collect), and a run whose every selected test is
// one the record marks failed (T255, L1725). Under the default 20 minutes a
// large project's such run is cut every time, pytest-testmon writes its
// record only at the end, and the next run selects the same tests again.
const FirstRunMinutes = 180

// EnvFailureMin is how many tests, every one of them failing and none
// passing, make a run's failures the environment's (the test services down),
// not the change's: such a run's record is not kept (Watch.Refusal).
const EnvFailureMin = 5

// testmonDir is the folder of Jhon's pytest-testmon records.
func testmonDir() string {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(project.Home(), ".cache")
	}
	return filepath.Join(cache, "jhon", "testmon")
}

// testmonFile is the record keyed by key.
func testmonFile(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(testmonDir(), hex.EncodeToString(sum[:8])+".db")
}

// legacyTestmonData is where a worktree kept its own record of a project
// before T255: keyed by the worktree's top level and the project's path.
func legacyTestmonData(top, projectPath string) string {
	return testmonFile(top + "\x00" + projectPath)
}

// repos is each top level's repository (repoOf), read once a process.
var repos sync.Map

// repoOf is the repository whose top level is top: its git common dir,
// shared by every worktree of it, symlinks resolved; "" outside git.
func repoOf(top string) string {
	if v, ok := repos.Load(top); ok {
		return v.(string)
	}
	common := gitCommonDir(top)
	if common != "" {
		if real, err := filepath.EvalSymlinks(common); err == nil {
			common = real
		}
	}
	repos.Store(top, common)
	return common
}

// TestmonPath is the file holding pytest-testmon's record of the project at
// projectPath in the repository whose top level is top. It is one per
// repository and project (T255): keyed by the repository's git common dir,
// so every worktree of it, a new one per change, narrows from the record
// the others built, instead of running every test of each affected project
// once to build its own. Outside git it is the folder's own. It writes
// nothing: TestmonData is the record a run uses.
func TestmonPath(top, projectPath string) string {
	common := ""
	if top != "" {
		common = repoOf(top)
	}
	if common == "" {
		return legacyTestmonData(top, projectPath)
	}
	return testmonFile(common + "\x00" + projectPath)
}

// TestmonData is TestmonPath for a run, which may write: when the shared
// record is missing and this worktree has one from before (keyed by its top
// level), that one is adopted, copied, never moved, once. A dry run, which
// writes nothing (T256), reads TestmonReadable instead.
func TestmonData(top, projectPath string) string {
	data := TestmonPath(top, projectPath)
	if old := legacyTestmonData(top, projectPath); old != data && !project.Exists(data) {
		adopt(old, data)
	}
	return data
}

// TestmonReadable is the file a run of the project at projectPath reads its
// record from, found without writing: the shared record (TestmonPath), or,
// while it is missing, the worktree's own from before, which the run would
// adopt (TestmonData).
func TestmonReadable(top, projectPath string) string {
	data := TestmonPath(top, projectPath)
	if old := legacyTestmonData(top, projectPath); old != data && !project.Exists(data) && project.Exists(old) {
		return old
	}
	return data
}

// LegacyTestmonRecords are the records every worktree of top's repository
// (`git worktree list`, top among them) kept of the project at projectPath
// before T255: any of them is adopted again by its worktree while the
// shared record is missing, so forgetting the record drops them all
// (T256). None outside git, where the folder's own record is the record.
func LegacyTestmonRecords(top, projectPath string) []string {
	data := TestmonPath(top, projectPath)
	var out []string
	for _, t := range append(worktreeTops(top), top) {
		if old := legacyTestmonData(t, projectPath); old != data && !slices.Contains(out, old) {
			out = append(out, old)
		}
	}
	return out
}

// worktreeTops are the top levels of every worktree of top's repository, in
// `git worktree list` order; nil when git cannot list them.
func worktreeTops(top string) []string {
	if top == "" {
		return nil
	}
	out, err := project.ReadGitCmd(top, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var tops []string
	for _, l := range strings.Split(string(out), "\n") {
		if dir, ok := strings.CutPrefix(l, "worktree "); ok && dir != "" {
			tops = append(tops, dir)
		}
	}
	return tops
}

// adopt makes the record old the record data, which is missing: a copy,
// linked into place so a record that appeared meanwhile is kept.
func adopt(old, data string) {
	if !project.Exists(old) || os.MkdirAll(filepath.Dir(data), 0o777) != nil {
		return
	}
	tmp, lock, err := privateCopy(old, data, "adopt")
	if err != nil {
		return
	}
	defer let(lock, tmp)
	defer drop(tmp)
	merge(tmp)
	shareable(tmp)
	if os.Link(tmp, data) == nil {
		if st, err := os.Stat(tmp + "-wal"); err == nil && st.Size() > 0 {
			_ = os.Rename(tmp+"-wal", data+"-wal")
		}
	}
}

// privateCopy is a new file beside data, `<data>.<pid>-<n>.<kind>` (n a
// random number, so two runs with one pid, each pid 2 in its own sandbox,
// never share it), holding a copy of the record src and of the write-ahead
// log beside it, and the lock this process holds on it until let goes of it
// (hold), which tells a later run's sweep it is a live run's. With no record
// at src the file is not there: the run makes it.
func privateCopy(src, data, kind string) (name string, lock *os.File, err error) {
	var f *os.File
	for range 100 {
		name = filepath.Join(filepath.Dir(data), filepath.Base(data)+"."+strconv.Itoa(os.Getpid())+"-"+runNumber()+"."+kind)
		if lock, err = hold(name); err != nil {
			return "", nil, err
		}
		if f, err = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600); err == nil {
			break
		}
		let(lock, name)
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	if f == nil {
		return "", nil, err
	}
	in, err := os.Open(src)
	if err != nil {
		f.Close()
		os.Remove(name)
		return name, lock, nil
	}
	defer in.Close()
	_, err = io.Copy(f, in)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = copyWAL(src, name)
	}
	if err != nil {
		drop(name)
		let(lock, name)
		return "", nil, err
	}
	return name, lock, nil
}

// runNumber is a random number, the part of a run's file name that makes it
// the run's own.
func runNumber() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[:])), 10)
}

// lockFile is the lock beside a run's file: `<file>.lock`.
func lockFile(file string) string { return file + ".lock" }

// hold makes and takes the lock beside a run's file, which this process
// holds while the file is its (T256, L1728): the kernel lets it go when the
// process ends, killed or not, and a process in another pid namespace (a
// sandboxed jhon is pid 2 and sees no host process) sees it held exactly as
// a host process does. Taken before the file is made, so a sweep never
// meets the file of a live run without its lock held; a lock a sweep removed
// between its making and its taking is made again.
func hold(file string) (*os.File, error) {
	for range 100 {
		f, err := os.OpenFile(lockFile(file), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return nil, err
		}
		if same(f, lockFile(file)) {
			return f, nil
		}
		f.Close()
	}
	return nil, errors.New("could not hold the lock of " + file)
}

// same is whether the open file f is still the one at path.
func same(f *os.File, path string) bool {
	a, err1 := f.Stat()
	b, err2 := os.Stat(path)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}

// let lets go of the lock hold took on file's, and removes it: the file is
// gone or published.
func let(lock *os.File, file string) {
	if lock == nil {
		return
	}
	if same(lock, lockFile(file)) {
		os.Remove(lockFile(file))
	}
	lock.Close()
}

// copyWAL copies the write-ahead log beside src, when it holds any, beside dst.
func copyWAL(src, dst string) error {
	b, err := os.ReadFile(src + "-wal")
	if err != nil || len(b) == 0 {
		return nil
	}
	return os.WriteFile(dst+"-wal", b, 0o666)
}

// mergeWAL merges a database's write-ahead log into it.
const mergeWAL = `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute("pragma wal_checkpoint(TRUNCATE)")
c.close()`

// merge folds the write-ahead log beside the record file, when it holds
// any, into the file, so the file is the whole record on its own; with no
// python3 the log stays, and publish moves it with the file.
func merge(file string) {
	if st, err := os.Stat(file + "-wal"); err != nil || st.Size() == 0 {
		return
	}
	_ = exec.Command("python3", "-I", "-c", mergeWAL, file).Run()
	if st, err := os.Stat(file + "-wal"); err == nil && st.Size() == 0 {
		os.Remove(file + "-wal")
	}
}

// sharedMode is the record's mode: every worktree's run, and the owner, read it.
const sharedMode = 0o644

// shareable gives a record file, and the write-ahead log beside it, the
// record's mode: a private copy is made 0600 (os.CreateTemp).
func shareable(file string) {
	for _, f := range []string{file, file + "-wal"} {
		if project.Exists(f) {
			_ = os.Chmod(f, sharedMode)
		}
	}
}

// publish makes the record file tmp the record data, in one rename: a reader
// opens the old record or the new one, never a part of either. The record is
// 0644 whatever tmp was (T256).
func publish(tmp, data string) bool {
	merge(tmp)
	shareable(tmp)
	// A write-ahead log belongs to the file it is beside: the old record's
	// goes, the new one's moves with it.
	for _, suffix := range sqliteBeside {
		os.Remove(data + suffix)
	}
	if os.Rename(tmp, data) != nil {
		return false
	}
	if st, err := os.Stat(tmp + "-wal"); err == nil && st.Size() > 0 {
		_ = os.Rename(tmp+"-wal", data+"-wal")
	}
	return true
}

// drop removes a record file and what sqlite keeps beside it.
func drop(file string) {
	for _, suffix := range append([]string{""}, sqliteBeside...) {
		os.Remove(file + suffix)
	}
}

// staleAge is how old a run's file with no lock beside it must be to be
// swept: a jhon from before locks (T255, T256's first cut) made it, and
// nothing tells whether it still runs; the pid in its name does not, since a
// run in the sandbox sees no host process and every sandboxed jhon is pid 2.
const staleAge = 24 * time.Hour

// runFile is a run's file beside a record and what sqlite keeps beside it:
// a first run's `<record>.<pid>-<n>.building`, a private copy
// `<record>.<pid>-<n>.run` or `.adopt`, and those of a jhon from before:
// `<record>.<pid>.building`, T255's `<record>.<n>.run`. Its lock is
// `<file>.lock` (lockOf).
var (
	runFile = lazyre.New(`^([0-9a-f]{16}\.db\.[0-9]+(?:-[0-9]+)?\.(?:building|run|adopt))(?:-wal|-shm|-journal)?$`)
	lockOf  = lazyre.New(`^([0-9a-f]{16}\.db\.[0-9]+-[0-9]+\.(?:building|run|adopt))\.lock$`)
)

// sweep removes from dir, the records' folder, the files a jhon killed while
// it ran (SIGKILL, a closed terminal) left beside a record (T256, L1728): a
// run's file (runFile) whose lock (hold) nobody holds, with that lock, and a
// lock with no file that nobody holds. A file whose lock is held is a live
// run's, whatever its age and the pid in its name, here or in another pid
// namespace. A file with no lock, a jhon's from before locks, goes only
// once it is older than staleAge. The records, their logs and cut markers
// stay.
func sweep(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	old := time.Now().Add(-staleAge)
	files := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if m := runFile.FindStringSubmatch(e.Name()); m != nil {
			files[filepath.Join(dir, m[1])] = true
		} else if m := lockOf.FindStringSubmatch(e.Name()); m != nil {
			files[filepath.Join(dir, m[1])] = true
		}
	}
	for file := range files {
		switch lock, held := takeOver(file); {
		case held:
		case lock != nil:
			drop(file)
			let(lock, file)
		case before(file, old):
			drop(file)
		}
	}
}

// takeOver takes the lock of a run's file when nobody holds it, the run that
// held it being gone: lock is then this process's, to let go once the file
// is removed. held is whether a live run holds it; with neither, the file
// has no lock.
func takeOver(file string) (lock *os.File, held bool) {
	f, err := os.OpenFile(lockFile(file), os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, true
	}
	if !same(f, lockFile(file)) {
		// Another sweep removed it meanwhile, and a run may have made it
		// again: left to the next sweep.
		f.Close()
		return nil, true
	}
	return f, false
}

// before is whether file and what sqlite keeps beside it were all last
// written before t.
func before(file string, t time.Time) bool {
	for _, suffix := range append([]string{""}, sqliteBeside...) {
		if st, err := os.Stat(file + suffix); err == nil && !st.ModTime().Before(t) {
			return false
		}
	}
	return true
}

// ranToEnd is whether a pytest run that exited with code (nil when it was
// cut) ran to its end: its tests passed (0) or some failed (1). 2 is pytest
// interrupted, 3 an internal error, 4 a usage error, 5 no test collected.
func ranToEnd(code *int) bool { return code != nil && (*code == 0 || *code == 1) }

// Private is a run's own copy of the shared record (T255): the run never
// writes the shared record in place, so two runs at once (two worktrees)
// never write one file, and a run cut short leaves it as it was.
type Private struct {
	data, file string
	// lock is the lock this process holds on file (hold).
	lock *os.File
	// had is whether the copy started from a record, of size and mtime.
	had  bool
	size int64
	mod  time.Time
}

// CheckOut copies the shared record at data to a private file beside it,
// for one run; with no record, the run starts from none. What a killed run
// left beside the records goes first (sweep).
func CheckOut(data string) (*Private, error) {
	if err := os.MkdirAll(filepath.Dir(data), 0o777); err != nil {
		return nil, err
	}
	sweep(filepath.Dir(data))
	file, lock, err := privateCopy(data, data, "run")
	if err != nil {
		return nil, err
	}
	p := &Private{data: data, file: file, lock: lock}
	if st, err := os.Stat(file); err == nil {
		p.had, p.size, p.mod = true, st.Size(), st.ModTime()
	}
	return p, nil
}

// File is the private copy.
func (p *Private) File() string { return p.file }

// Command is a command that names the shared record (Precise) told to write
// the private copy instead; any other command as it is.
func (p *Private) Command(command string) string {
	return strings.ReplaceAll(command, "TESTMON_DATAFILE="+shq(p.data)+" ", "TESTMON_DATAFILE="+shq(p.file)+" ")
}

// changed is whether the run wrote the copy.
func (p *Private) changed() bool {
	st, err := os.Stat(p.file)
	if err != nil {
		return false
	}
	if !p.had || st.Size() != p.size || !st.ModTime().Equal(p.mod) {
		return true
	}
	wal, err := os.Stat(p.file + "-wal")
	return err == nil && wal.Size() > 0
}

// Finish ends the run that exited with code (nil when it was cut) and whose
// output w watched (nil when none was): the copy replaces the shared record,
// atomically, when pytest ran to its end and wrote it, unless its failures
// are the environment's (refused, Watch.Refusal); it is dropped otherwise,
// and the shared record left as it was. The last run to finish wins: a
// record of other code only makes pytest-testmon select more, never less.
func (p *Private) Finish(code *int, w *Watch) (published bool, refused string) {
	defer let(p.lock, p.file)
	defer drop(p.file)
	if !ranToEnd(code) {
		return false, ""
	}
	if refused = w.Refusal(); refused != "" {
		return false, refused
	}
	if !p.changed() {
		return false, ""
	}
	return publish(p.file, p.data), ""
}

// Forget drops the record at data, what sqlite keeps beside it and its cut
// first run's marker; removed is whether there was a record.
func Forget(data string) (removed bool) {
	removed = project.Exists(data)
	drop(data)
	os.Remove(cutFile(data))
	return removed
}

// CountTests is how many test functions (`def test…`, `async def test…`)
// the test modules (test_*.py, *_test.py) of the project at projectPath
// under root define, and in how many modules: what a run of all its tests
// runs, parametrized cases counted once.
func CountTests(root, projectPath string) (tests, modules int) {
	dir := filepath.Join(root, projectPath)
	_ = filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if path != dir && (strings.HasPrefix(e.Name(), ".") || slices.Contains(skippedDirs, e.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !testFile.MatchString(e.Name()) {
			return nil
		}
		if info, err := e.Info(); err != nil || info.Size() > maxReaderBytes {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if n := len(testDef.FindAllStringIndex(string(b), -1)); n > 0 {
			tests, modules = tests+n, modules+1
		}
		return nil
	})
	return tests, modules
}

// testDef is a test function's definition.
var testDef = lazyre.New(`(?m)^[ \t]*(?:async[ \t]+)?def[ \t]+test\w*[ \t]*\(`)

// Watch reads a pytest run's output as it streams, for what its tail may no
// longer hold: how many tests it selected and how far it got, its summary,
// and whether its failures are the environment's.
type Watch struct {
	mu   sync.Mutex
	line []byte
	// selected is -1 until pytest says what it collected.
	selected, percent         int
	passed, failed, errors    int
	summary                   bool
	refusedBy, collectionLine string
	// refusedIn is the first refusal said in the failures (failureLine),
	// and inFailures whether the output is in their sections.
	refusedIn  string
	inFailures bool
}

// NewWatch is a Watch of a run that has not started.
func NewWatch() *Watch { return &Watch{selected: -1, percent: -1} }

// maxLine bounds the part of one line a Watch keeps.
const maxLine = 4 << 10

func (w *Watch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range p {
		if c == '\n' || c == '\r' {
			w.scan(string(w.line))
			w.line = w.line[:0]
			continue
		}
		if len(w.line) < maxLine {
			w.line = append(w.line, c)
		}
	}
	return len(p), nil
}

var (
	// collectedLine is pytest's `collected 605 items / 28 deselected / 577 selected`.
	collectedLine  = lazyre.New(`\bcollected (\d+) items?\b`)
	deselectedPart = lazyre.New(`/ (\d+) deselected\b`)
	selectedPart   = lazyre.New(`/ (\d+) selected\b`)
	// percentLine is a progress line's `[ 24%]`.
	percentLine = lazyre.New(`\[\s*(\d{1,3})%\]\s*$`)
	// summaryLine is pytest's last line, `=== 3 failed, 2 passed in 1.20s ===`.
	summaryLine = lazyre.New(`^=*\s*(\d+ [a-z]+(?:, \d+ [a-z]+)*) in [0-9.]+s\b`)
	summaryPart = lazyre.New(`(\d+) (passed|failed|errors?)\b`)
	// serviceDown is a test service refusing or not answering: a database,
	// a cache or a broker that is not up.
	serviceDown = lazyre.New(`(?i)connection refused|could not connect to server|connect call failed|error 111 connecting|temporary failure in name resolution`)
	// collectionError is pytest failing to import a test module.
	collectionError = lazyre.New(`errors? during collection|ERROR collecting `)
	// sectionLine is a pytest section's header, `==== FAILURES ====`.
	sectionLine = lazyre.New(`^=+ (.+?) =+$`)
	// failureLine is a line of a failure wherever it is: a traceback's
	// error line, or the short summary's.
	failureLine = lazyre.New(`^(?:E |FAILED |ERROR )`)
)

// failureSections are the sections of pytest's output that hold only the
// failures and errors.
var failureSections = []string{"FAILURES", "ERRORS", "short test summary info"}

func (w *Watch) scan(s string) {
	if m := sectionLine.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		w.inFailures = slices.Contains(failureSections, m[1])
	}
	if m := collectedLine.FindStringSubmatch(s); m != nil {
		collected, _ := strconv.Atoi(m[1])
		w.selected = collected
		if d := deselectedPart.FindStringSubmatch(s); d != nil {
			n, _ := strconv.Atoi(d[1])
			w.selected = collected - n
		}
		if sel := selectedPart.FindStringSubmatch(s); sel != nil {
			w.selected, _ = strconv.Atoi(sel[1])
		}
	}
	if m := percentLine.FindStringSubmatch(s); m != nil {
		w.percent, _ = strconv.Atoi(m[1])
	}
	if m := summaryLine.FindStringSubmatch(s); m != nil {
		w.summary, w.passed, w.failed, w.errors = true, 0, 0, 0
		for _, p := range summaryPart.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			switch p[2] {
			case "passed":
				w.passed = n
			case "failed":
				w.failed = n
			default:
				w.errors = n
			}
		}
	}
	if serviceDown.MatchString(s) {
		line := shorten(strings.TrimSpace(s), 160)
		if w.refusedBy == "" {
			w.refusedBy = line
		}
		if w.refusedIn == "" && (w.inFailures || failureLine.MatchString(s)) {
			w.refusedIn = line
		}
	}
	if w.collectionLine == "" && collectionError.MatchString(s) {
		w.collectionLine = shorten(strings.TrimSpace(s), 160)
	}
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Refusal is why the run's record is not kept, its failures being the test
// environment's rather than the change's (T255, L1725): its output says a
// test service refused the connection in its failures, or anywhere when no
// test passed (a passing test may log a refusal it retries, T256), pytest
// could not collect a test module, or every one of at least EnvFailureMin
// tests failed and none passed. "" when none is the case, and for a nil Watch. A record
// such a run wrote marks every test it ran failed, and pytest-testmon runs
// every failed test again whatever changed.
func (w *Watch) Refusal() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.line) > 0 {
		w.scan(string(w.line))
		w.line = w.line[:0]
	}
	bad := w.failed + w.errors
	switch {
	case w.refusedIn != "" && bad > 0:
		return "its output says a test service refused the connection (" + w.refusedIn + ")"
	case w.refusedBy != "" && w.passed == 0 && (bad > 0 || !w.summary):
		return "its output says a test service refused the connection (" + w.refusedBy + ")"
	case w.collectionLine != "":
		return "pytest could not collect every test module (" + w.collectionLine + ")"
	case w.summary && w.passed == 0 && bad >= EnvFailureMin:
		return fmt.Sprintf("every one of its %d tests failed", bad)
	}
	return ""
}

// Progress is how far a cut run got, as its output said: "pytest got
// through about 138 of its 577 selected tests (24%)"; "" when pytest did not
// say what it selected.
func (w *Watch) Progress() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.selected < 0 {
		return ""
	}
	if w.percent < 0 {
		return fmt.Sprintf("pytest selected %d tests and finished none", w.selected)
	}
	return fmt.Sprintf("pytest got through about %d of its %d selected tests (%d%%)", w.selected*w.percent/100, w.selected, w.percent)
}
