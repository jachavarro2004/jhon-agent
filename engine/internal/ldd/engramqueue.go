package ldd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// This file is Jhon's Engram queue (T222 S1b, L8). Inside Jhon's sandbox
// Engram's folder stays read-only: a writable one lets a sandboxed command
// steer what Engram does outside the sandbox (L5, L7). So a state record
// (state.go) that cannot reach Engram there is queued, one file per record,
// in <git common dir>/jhon/engram-queue/, which a sandboxed command may
// write, and Jhon, running outside the sandbox (any jhon command, the agent
// hooks among them), saves each queued record to Engram and removes it.
//
// What is in the queue is a sandboxed command's to write, so nothing in it
// is trusted: a record is saved only when it is a well-formed state record
// of Jhon's own (`ldd state: <change>` under the topic `ldd-state/<change>`,
// with the state's content, naming the repository's main worktree), always
// from the main worktree, and any other entry is moved to refused/ (the
// newest refusedMax kept), reported and never saved.
//
// No link is followed anywhere on the way (L11 B1, L14 B1′): the git common
// dir is opened with openat2 and RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS,
// so the kernel refuses a link in any of its components (a linked
// worktree's common dir is a path the sandbox wrote, under folders it may
// swap for a link); from there jhon/ and engram-queue/ are each opened
// relative to the one before with O_NOFOLLOW|O_DIRECTORY, and every read,
// rename, removal and the refused/ folder are relative to those
// descriptors, never to a path. Engram runs in the main worktree, opened
// the same way and entered through its descriptor. Without openat2 (Linux
// before 5.6), or where a seccomp filter refuses openat2 (EPERM), the queue
// fails closed: nothing is read, written or saved.

// queueMaxFile is the largest queued record, in bytes; queueBatch the most
// records one flush saves, and the most entries it refuses; refusedMax the
// most entries refused/ keeps; FlushDeadline bounds one flush's saves.
const (
	queueMaxFile  = 64 << 10
	queueBatch    = 32
	refusedMax    = 100
	FlushDeadline = 5 * time.Second
)

// queueTimeFormat names a record by when it was queued, so the names sort
// in that order; a record under write is .tmp-<name>, and one left so for
// more than staleWrite is removed.
const (
	queueTimeFormat = "20060102T150405.000000000Z"
	staleWrite      = time.Minute
)

// queueName is a record's file name: its time, then its change.
var queueName = lazyre.New(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-([A-Za-z0-9][A-Za-z0-9._-]*)\.json$`)

// queuedState is a queued record: what `engram save` takes, and the main
// worktree it was queued for.
type queuedState struct {
	Title   string `json:"title"`
	Topic   string `json:"topic"`
	Content string `json:"content"`
	Project string `json:"project"`
}

// Refusal is an entry a flush refused: its name (the queue folder's path
// for the queue itself) and why. The name is a sandboxed command's to
// choose: a caller that prints it quotes it.
type Refusal struct{ Name, Why string }

// FlushReport is what one flush did: the changes whose records it saved,
// the entries it refused, how many records (entries named as one) it left
// for the next flush, and why it did not save them all ("" when it did).
type FlushReport struct {
	Saved   []string
	Refused []Refusal
	Left    int
	Failed  string
	// EngramAsked is whether Failed is Engram's own answer: it was asked to
	// save a record and did not take it. Otherwise the flush stopped before
	// asking it (no time left, no engram, no main worktree).
	EngramAsked bool
	// Closed is why the queue was not opened at all, failing closed: this
	// kernel has no openat2 (ErrNoOpenat2), or a seccomp filter refuses it
	// (ErrOpenat2Refused).
	Closed string
}

// flushBudget is FlushDeadline; a test shortens it.
var flushBudget = FlushDeadline

// EngramQueue is the queue folder of the repository top is in, read from
// the disk as the sandbox reads it (project.ReadRepo); "" where no git
// common dir is proven.
func EngramQueue(top string) string {
	common := project.ReadRepo(top).Common
	if common == "" {
		return ""
	}
	return filepath.Join(common, "jhon", "engram-queue")
}

// dirFlags open a folder, never a link to one.
const dirFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC

// openHow is openat2's struct open_how.
type openHow struct{ Flags, Mode, Resolve uint64 }

// openat2's number (amd64 and arm64 share it) and its resolve flags.
const (
	sysOpenat2          = 437
	resolveNoMagiclinks = 0x02
	resolveNoSymlinks   = 0x04
)

// ErrNoOpenat2 is the queue failing closed on a kernel with no openat2.
var ErrNoOpenat2 = errors.New("this kernel has no openat2 (Linux 5.6 or newer), which the Engram queue needs to follow no link")

// ErrOpenat2Refused is the queue failing closed where openat2 answers
// EPERM: a seccomp filter refuses it (an older container profile answers
// so a syscall it does not know). The queue is not at fault, and nothing
// in it is discarded.
var ErrOpenat2Refused = errors.New("openat2 is refused here (operation not permitted: a seccomp filter, as a container's, blocks it), which the Engram queue needs to follow no link")

// OpenCommonDir opens the folder path names, absolute, as an O_PATH
// descriptor, with no link in any of its components followed (openat2,
// RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS). A test replaces it.
var OpenCommonDir = func(path string) (int, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return -1, err
	}
	how := openHow{Flags: oPath | syscall.O_DIRECTORY | syscall.O_CLOEXEC, Resolve: resolveNoSymlinks | resolveNoMagiclinks}
	cwd := atFdcwd
	fd, _, e := syscall.Syscall6(sysOpenat2, uintptr(cwd), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	if e != 0 {
		return -1, e
	}
	return int(fd), nil
}

// atFdcwd is AT_FDCWD.
const atFdcwd = -100

// openNoLink opens path with OpenCommonDir, an error naming it.
func openNoLink(path string) (int, error) {
	fd, err := OpenCommonDir(path)
	switch {
	case err == syscall.ENOSYS:
		return -1, ErrNoOpenat2
	case err == syscall.EPERM:
		return -1, ErrOpenat2Refused
	case err == syscall.ELOOP || err == syscall.ENOTDIR || err == syscall.EXDEV:
		return -1, errors.New(path + " is reached through a link or is not a folder")
	case err != nil:
		return -1, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return fd, nil
}

// openQueueFd opens queue (<common>/jhon/engram-queue) as a descriptor: the
// common dir with no link in its path (openNoLink), then jhon/ and
// engram-queue/ each relative to the one before, with no link followed;
// with create, jhon/ and engram-queue/ are made where missing. An error
// wraps fs.ErrNotExist where a folder is missing, and is ErrNoOpenat2 or
// ErrOpenat2Refused where the common dir cannot be opened so.
func openQueueFd(queue string, create bool) (int, error) {
	at := filepath.Dir(filepath.Dir(queue))
	fd, err := openNoLink(at)
	if err != nil {
		return -1, err
	}
	for _, name := range []string{"jhon", "engram-queue"} {
		at = filepath.Join(at, name)
		next, err := syscall.Openat(fd, name, dirFlags, 0)
		if err == syscall.ENOENT && create {
			if err = syscall.Mkdirat(fd, name, 0o700); err == nil || err == syscall.EEXIST {
				next, err = syscall.Openat(fd, name, dirFlags, 0)
			}
		}
		syscall.Close(fd)
		switch {
		case err == syscall.ELOOP || err == syscall.ENOTDIR:
			return -1, errors.New(at + " is a link or not a folder")
		case err != nil:
			return -1, &fs.PathError{Op: "open", Path: at, Err: err}
		}
		fd = next
	}
	return fd, nil
}

// namesAt are the names in the folder dirfd is, read until deadline (the
// zero time is none): past it, the names read so far.
func namesAt(dirfd int, deadline time.Time) ([]string, error) {
	fd, err := syscall.Openat(dirfd, ".", dirFlags, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), ".")
	defer f.Close()
	var names []string
	for deadline.IsZero() || time.Now().Before(deadline) {
		chunk, err := f.Readdirnames(256)
		names = append(names, chunk...)
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return names, err
		}
	}
	return names, nil
}

// lstatAt is what name in dirfd is, a link itself rather than its target.
func lstatAt(dirfd int, name string) (os.FileInfo, error) {
	fd, err := syscall.Openat(dirfd, name, oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	return f.Stat()
}

// atRemoveDir is unlinkat's flag that removes a folder; oPath is open's
// O_PATH (amd64 and arm64, the arches Jhon ships for), which the syscall
// package does not name.
const (
	atRemoveDir = 0x200
	oPath       = 0x200000
)

// removeAt removes name from dirfd, a folder with what it holds (to depth
// 16), following no link, until deadline (the zero time is none).
func removeAt(dirfd int, name string, depth int, deadline time.Time) error {
	fi, err := lstatAt(dirfd, name)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return syscall.Unlinkat(dirfd, name)
	}
	if depth > 16 {
		return errors.New("too deep")
	}
	sub, err := syscall.Openat(dirfd, name, dirFlags, 0)
	if err != nil {
		return err
	}
	names, _ := namesAt(sub, deadline)
	for _, n := range names {
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		removeAt(sub, n, depth+1, deadline)
	}
	syscall.Close(sub)
	p, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	if _, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(dirfd), uintptr(unsafe.Pointer(p)), atRemoveDir); e != 0 {
		return e
	}
	return nil
}

// queueState writes s as a record in top's queue, atomically: a .tmp- file
// created anew in the queue's descriptor, never through a link, then
// renamed into place. The record names the repository's main worktree,
// the one it is saved from. It returns the queue folder, and why the
// record could not be written.
func queueState(top string, s ChangeState) (string, error) {
	queue := EngramQueue(top)
	if queue == "" {
		return "", errors.New("no git common dir holds it")
	}
	if !changeName.MatchString(s.Change) {
		return queue, errors.New(s.Change + " is not a change name")
	}
	main := project.RepoMain(project.ReadRepo(top))
	b, err := json.Marshal(queuedState{Title: StateTitle(s.Change), Topic: StateTopic(s.Change), Content: s.Content(), Project: main})
	if err != nil {
		return queue, err
	}
	if len(b) > queueMaxFile {
		return queue, errors.New("the record is larger than " + strconv.Itoa(queueMaxFile) + " bytes")
	}
	qfd, err := openQueueFd(queue, true)
	if err != nil {
		return queue, err
	}
	defer syscall.Close(qfd)
	name := time.Now().UTC().Format(queueTimeFormat) + "-" + s.Change + ".json"
	tmp := ".tmp-" + name
	fd, err := syscall.Openat(qfd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return queue, err
	}
	f := os.NewFile(uintptr(fd), tmp)
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = syscall.Renameat(qfd, tmp, qfd, name)
	}
	if err != nil {
		syscall.Unlinkat(qfd, tmp)
	}
	return queue, err
}

// FlushEngramQueue saves the records queued in root's repository to Engram,
// outside Jhon's sandbox only (inside it does nothing): at most queueBatch
// of them, oldest first, each from the main worktree. A well-formed record
// is saved and removed; any other entry is moved to refused/ and reported,
// at most queueBatch of them per flush, the rest left for the next; the
// entries named as a record come first, the others after, so no entry
// sorted before a record keeps it from being saved (T227). A write left
// half-done for a minute is removed; a record Engram does not take stays
// for the next flush, and stops its saves. FlushDeadline bounds it all,
// the listing and the removals too: past it the flush stops and leaves the
// rest for the next one. A queue reached through a link is reported and
// not read; without openat2, or where a seccomp filter refuses it, the
// queue is not opened (Closed).
func FlushEngramQueue(root string) FlushReport {
	var r FlushReport
	if sandbox.Inside() {
		return r
	}
	queue := EngramQueue(root)
	if queue == "" {
		return r
	}
	deadline := time.Now().Add(flushBudget)
	noTime := "no time left within " + FlushDeadline.String()
	qfd, err := openQueueFd(queue, false)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return r
	case errors.Is(err, ErrNoOpenat2), errors.Is(err, ErrOpenat2Refused):
		r.Closed = err.Error()
		return r
	case err != nil:
		r.Refused = []Refusal{{queue, "the queue is a link or not a folder; nothing in it was read"}}
		return r
	}
	defer syscall.Close(qfd)
	names, err := queueEntries(qfd, true, deadline)
	if err != nil {
		r.Failed = err.Error()
		return r
	}
	records := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return !queueName.MatchString(n) })
	others := slices.DeleteFunc(names, queueName.MatchString)
	if !time.Now().Before(deadline) {
		r.Failed, r.Left = noTime, len(records)
		return r
	}
	bin := EngramBin()
	if bin == "" {
		r.Failed = "engram not found"
	}
	// Engram runs in the main worktree, entered through a descriptor opened
	// with no link followed, not by its path.
	main := project.RepoMain(project.ReadRepo(root))
	dir := ""
	if mfd, err := openNoLink(main); err == nil {
		defer syscall.Close(mfd)
		dir = "/proc/self/fd/" + strconv.Itoa(mfd)
	} else if r.Failed == "" {
		r.Failed = err.Error()
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	rfd := -1
	defer func() {
		if rfd >= 0 {
			capRefused(rfd, deadline)
			syscall.Close(rfd)
		}
	}()
	// refuse moves name to refused/ and reports it, while this flush has
	// refused fewer than queueBatch entries; past that it leaves name for
	// the next flush and is false.
	refuse := func(name, why string) bool {
		if len(r.Refused) == queueBatch {
			return false
		}
		if rfd < 0 {
			rfd = refusedFd(qfd)
		}
		if rfd >= 0 {
			syscall.Renameat(qfd, name, rfd, name)
		}
		r.Refused = append(r.Refused, Refusal{name, why})
		return true
	}
	for _, name := range records {
		if r.Failed == "" && !time.Now().Before(deadline) {
			r.Failed = noTime
		}
		if len(r.Saved) == queueBatch || r.Failed != "" {
			r.Left++
			continue
		}
		rec, change, why := readQueued(qfd, name, main)
		if why != "" {
			refuse(name, why)
			continue
		}
		if stderr, err := engramSave(ctx, bin, dir, rec.Title, rec.Content, rec.Topic); err != nil {
			r.Failed, r.EngramAsked = saveFailure(ctx, err, stderr, FlushDeadline), true
			r.Left++
			continue
		}
		syscall.Unlinkat(qfd, name)
		r.Saved = append(r.Saved, change)
	}
	// The entries named as no record, refused by their name alone.
	for _, name := range others {
		if !time.Now().Before(deadline) {
			break
		}
		if _, _, why := readQueued(qfd, name, main); !refuse(name, why) {
			break
		}
	}
	return r
}

// refusedFd is the queue's refused/ folder, made if missing; -1 where it
// is a link or cannot be made.
func refusedFd(qfd int) int {
	if err := syscall.Mkdirat(qfd, "refused", 0o700); err != nil && err != syscall.EEXIST {
		return -1
	}
	fd, err := syscall.Openat(qfd, "refused", dirFlags, 0)
	if err != nil {
		return -1
	}
	return fd
}

// lstatRefused is lstatAt for capRefused's ages; a test slows it.
var lstatRefused = lstatAt

// capRefused removes the oldest entries of refused/ beyond refusedMax,
// until deadline: past it, it looks at no further entry's age (T227) and
// removes nothing more.
func capRefused(rfd int, deadline time.Time) {
	names, err := namesAt(rfd, deadline)
	if err != nil || len(names) <= refusedMax {
		return
	}
	type aged struct {
		name string
		at   time.Time
	}
	var all []aged
	for _, n := range names {
		if time.Now().After(deadline) {
			return
		}
		at := time.Time{}
		if fi, err := lstatRefused(rfd, n); err == nil {
			at = fi.ModTime()
		}
		all = append(all, aged{n, at})
	}
	slices.SortFunc(all, func(a, b aged) int { return a.at.Compare(b.at) })
	for _, a := range all[:len(all)-refusedMax] {
		if time.Now().After(deadline) {
			return
		}
		removeAt(rfd, a.name, 0, deadline)
	}
}

// EngramQueueCounts are how many records wait in root's queue (names of a
// record) and how many entries were refused, read with no link followed;
// 0 and 0 where there is no queue folder, and the error where it cannot be
// read.
func EngramQueueCounts(root string) (queued, refused int, err error) {
	queue := EngramQueue(root)
	if queue == "" {
		return 0, 0, nil
	}
	qfd, err := openQueueFd(queue, false)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	defer syscall.Close(qfd)
	deadline := time.Now().Add(flushBudget)
	names, err := queueEntries(qfd, false, deadline)
	if err != nil {
		return 0, 0, err
	}
	for _, n := range names {
		if queueName.MatchString(n) {
			queued++
		}
	}
	if rfd, err := syscall.Openat(qfd, "refused", dirFlags, 0); err == nil {
		entries, _ := namesAt(rfd, deadline)
		syscall.Close(rfd)
		refused = len(entries)
	}
	return queued, refused, nil
}

// queueEntries are the entries of the queue folder by name, but refused/
// and a record being written (.tmp-); with clean, a .tmp- one older than
// staleWrite is removed; all of it until deadline.
func queueEntries(qfd int, clean bool, deadline time.Time) ([]string, error) {
	all, err := namesAt(qfd, deadline)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range all {
		switch {
		case name == "refused":
		case strings.HasPrefix(name, ".tmp-"):
			if fi, err := lstatAt(qfd, name); clean && err == nil && time.Since(fi.ModTime()) > staleWrite && time.Now().Before(deadline) {
				removeAt(qfd, name, 0, deadline)
			}
		default:
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// readQueued is the record name holds and its change, or why it is no
// well-formed state record of Jhon's own for the main worktree.
func readQueued(qfd int, name, main string) (queuedState, string, string) {
	var rec queuedState
	m := queueName.FindStringSubmatch(name)
	if m == nil {
		return rec, "", "its name is not <time>-<change>.json"
	}
	change := m[1]
	if fi, err := lstatAt(qfd, name); err != nil || !fi.Mode().IsRegular() {
		return rec, change, "it is not a regular file"
	}
	fd, err := syscall.Openat(qfd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return rec, change, "it is not a regular file"
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return rec, change, "it is not a regular file"
	}
	b, err := io.ReadAll(io.LimitReader(f, queueMaxFile+1))
	if err != nil {
		return rec, change, "it could not be read"
	}
	if len(b) > queueMaxFile {
		return rec, change, "it is larger than " + strconv.Itoa(queueMaxFile) + " bytes"
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return rec, change, "it is not a queued state record (title, topic, content, project)"
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return rec, change, "it is not a queued state record (title, topic, content, project)"
	}
	switch {
	case rec.Title != StateTitle(change):
		return rec, change, "its title is not " + StateTitle(change)
	case rec.Topic != StateTopic(change):
		return rec, change, "its topic is not " + StateTopic(change)
	case !strings.HasPrefix(rec.Content, "**What**: the LDD state of change "+change+", ") || !utf8.ValidString(rec.Content) || strings.ContainsRune(rec.Content, 0):
		return rec, change, "its content is not the LDD state of " + change
	case main == "" || rec.Project != main:
		return rec, change, "its project is not this repository's main worktree"
	}
	return rec, change, ""
}

// engramSave runs `engram save` of a state record from dir, within ctx,
// and gives its stderr. PWD is left out of its environment: dir may be a
// descriptor's /proc/self/fd path, which means nothing to engram itself.
func engramSave(ctx context.Context, bin, dir, title, content, topic string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "save", title, content, "--type", StateType, "--topic", topic)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "PWD=") })
	cmd.WaitDelay = 100 * time.Millisecond
	stderr := &capped{n: engramStderrMax}
	cmd.Stderr = stderr
	err := cmd.Run()
	return stderr.String(), err
}

// saveFailure is why a save that failed with err, having written stderr,
// did not reach Engram within deadline: its first stderr line, else the
// error.
func saveFailure(ctx context.Context, err error, stderr string, deadline time.Duration) string {
	if ctx.Err() != nil {
		return "no answer within " + deadline.String()
	}
	if why := strings.TrimSpace(strings.SplitN(stderr, "\n", 2)[0]); why != "" {
		return why
	}
	return err.Error()
}

// readOnlyFailure is whether a failed Engram call's whole stderr says its
// folder is on a read-only file system: not only the first line, nor the
// 200 characters a note quotes of it (L5 B2).
func readOnlyFailure(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "read-only file system")
}

// engramFolder is the folder Engram writes: ENGRAM_DATA_DIR, else
// ~/.engram.
func engramFolder() string {
	if d := os.Getenv("ENGRAM_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(project.Home(), ".engram")
}

// sandboxedFolder is why an Engram search failed, with, inside Jhon's
// sandbox when its whole stderr says read-only file system, that the
// sandbox keeps Engram's folder read-only and that Jhon's own state saves
// are queued (T222 S3, S1b). Outside the sandbox, or for any other
// failure, why as it is.
func sandboxedFolder(why, stderr string) string {
	if !sandbox.Inside() || !readOnlyFailure(stderr) {
		return why
	}
	return why + "; " + fmt.Sprintf("Jhon's sandbox keeps Engram's folder %s read-only: Jhon's own state saves are queued; Jhon saves them outside the sandbox", engramFolder())
}
