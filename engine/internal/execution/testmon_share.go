package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// TestmonData is the file holding pytest-testmon's record of the project at
// projectPath in the repository whose top level is top. It is one per
// repository and project (T255): keyed by the repository's git common dir,
// so every worktree of it, a new one per change, narrows from the record
// the others built, instead of running every test of each affected project
// once to build its own. Outside git it is the folder's own.
//
// When the shared record is missing and this worktree has one from before
// (keyed by its top level), that one is adopted: copied, never moved, once.
func TestmonData(top, projectPath string) string {
	common := ""
	if top != "" {
		common = repoOf(top)
	}
	if common == "" {
		return legacyTestmonData(top, projectPath)
	}
	data := testmonFile(common + "\x00" + projectPath)
	if !project.Exists(data) {
		adopt(legacyTestmonData(top, projectPath), data)
	}
	return data
}

// LegacyTestmonData is the record the worktree at top kept of the project at
// projectPath before T255, which TestmonData adopts when the shared record
// is missing; "" when it is the shared record itself (outside git).
func LegacyTestmonData(top, projectPath string) string {
	if old := legacyTestmonData(top, projectPath); old != TestmonData(top, projectPath) {
		return old
	}
	return ""
}

// adopt makes the record old the record data, which is missing: a copy,
// linked into place so a record that appeared meanwhile is kept.
func adopt(old, data string) {
	if !project.Exists(old) || os.MkdirAll(filepath.Dir(data), 0o777) != nil {
		return
	}
	tmp, err := privateCopy(old, data, "adopt")
	if err != nil {
		return
	}
	defer drop(tmp)
	merge(tmp)
	if os.Link(tmp, data) == nil {
		if st, err := os.Stat(tmp + "-wal"); err == nil && st.Size() > 0 {
			_ = os.Rename(tmp+"-wal", data+"-wal")
		}
	}
}

// privateCopy is a new file beside data, `<data>.<n>.<kind>`, holding a copy
// of the record src and of the write-ahead log beside it. With no record at
// src the file is not there: the run makes it.
func privateCopy(src, data, kind string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(data), filepath.Base(data)+".*."+kind)
	if err != nil {
		return "", err
	}
	name := f.Name()
	in, err := os.Open(src)
	if err != nil {
		f.Close()
		os.Remove(name)
		return name, nil
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
		return "", err
	}
	return name, nil
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

// publish makes the record file tmp the record data, in one rename: a reader
// opens the old record or the new one, never a part of either.
func publish(tmp, data string) bool {
	merge(tmp)
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

// ranToEnd is whether a pytest run that exited with code (nil when it was
// cut) ran to its end: its tests passed (0) or some failed (1). 2 is pytest
// interrupted, 3 an internal error, 4 a usage error, 5 no test collected.
func ranToEnd(code *int) bool { return code != nil && (*code == 0 || *code == 1) }

// Private is a run's own copy of the shared record (T255): the run never
// writes the shared record in place, so two runs at once (two worktrees)
// never write one file, and a run cut short leaves it as it was.
type Private struct {
	data, file string
	// had is whether the copy started from a record, of size and mtime.
	had  bool
	size int64
	mod  time.Time
}

// CheckOut copies the shared record at data to a private file beside it,
// for one run; with no record, the run starts from none.
func CheckOut(data string) (*Private, error) {
	if err := os.MkdirAll(filepath.Dir(data), 0o777); err != nil {
		return nil, err
	}
	file, err := privateCopy(data, data, "run")
	if err != nil {
		return nil, err
	}
	p := &Private{data: data, file: file}
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
)

func (w *Watch) scan(s string) {
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
	if w.refusedBy == "" && serviceDown.MatchString(s) {
		w.refusedBy = shorten(strings.TrimSpace(s), 160)
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
// test service refused the connection while tests failed, pytest could not
// collect a test module, or every one of at least EnvFailureMin tests failed
// and none passed. "" when none is the case, and for a nil Watch. A record
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
	case w.refusedBy != "" && (bad > 0 || !w.summary):
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
