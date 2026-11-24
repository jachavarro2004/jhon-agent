package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// lastFlush is what this command's flush did, for doctor; queueErr is
// where the flush's lines go (a test's buffer).
var (
	lastFlush ldd.FlushReport
	queueErr  io.Writer = os.Stderr
)

// kernelFix is the fix for a kernel with no openat2 (ldd.ErrNoOpenat2).
const kernelFix = "a Linux kernel 5.6 or newer"

// flushEngramQueue saves the state records Jhon queued inside its sandbox
// (T222 S1b, ldd.FlushEngramQueue) before a jhon command run outside it:
// every command, the agents' hooks among them, so the next tool call after
// a sandboxed `jhon executions verify` saves its state. dir is the folder
// whose repository's queue it flushes: a hook's payload cwd (pi runs its
// guards from anywhere), else "" for the working directory. It costs one
// walk up to the repository and one open where nothing is queued, and does
// nothing inside the sandbox or with Engram off. A refused entry, and once
// the records Engram did not take, are lines on stderr, never on stdout,
// which a hook answers on; a name a sandboxed command chose is quoted.
func flushEngramQueue(dir string) {
	if sandbox.Inside() {
		return
	}
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return
		}
	}
	top := project.ReadRepo(dir).Top
	if top == "" {
		return
	}
	queue := ldd.EngramQueue(top)
	if queue == "" {
		return
	}
	if _, err := os.Lstat(queue); err != nil || !engramOn() {
		return
	}
	lastFlush = ldd.FlushEngramQueue(top)
	if lastFlush.Closed != "" {
		fmt.Fprintf(queueErr, "jhon: the Engram queue was not read: %s; fix: %s\n", lastFlush.Closed, kernelFix)
	}
	for _, r := range lastFlush.Refused {
		fmt.Fprintf(queueErr, "jhon: the Engram queue refused %q: %s; it was never saved\n", r.Name, r.Why)
	}
	switch {
	case lastFlush.Failed == "":
	case lastFlush.Left == 1:
		fmt.Fprintf(queueErr, "jhon: the Engram queue kept 1 record for the next jhon command: Engram did not take it (%q)\n", lastFlush.Failed)
	default:
		fmt.Fprintf(queueErr, "jhon: the Engram queue kept %d records for the next jhon command: Engram did not take them (%q)\n", lastFlush.Left, lastFlush.Failed)
	}
}

// engramQueue is doctor's word on the Engram queue of the project here,
// under the sandbox section, once this command's flush ran: why Engram did
// not take the queued records, how many wait and how many entries were
// refused, or that the queue cannot be read; nothing where there is no
// queue folder. Every path is quoted: a linked worktree's common dir is a
// path a sandboxed command wrote.
func (d doctor) engramQueue() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	top := project.ReadRepo(dir).Top
	queue := ldd.EngramQueue(top)
	if top == "" || queue == "" {
		return
	}
	if _, err := os.Lstat(queue); err != nil {
		return
	}
	queued, refused, err := ldd.EngramQueueCounts(top)
	switch {
	case errors.Is(err, ldd.ErrNoOpenat2):
		d.o.Warn(fmt.Sprintf("Engram queue %q was not read: %s; fix: %s", queue, err, kernelFix))
		return
	case err != nil:
		d.o.Warn(fmt.Sprintf("Engram queue %q cannot be read: %q; nothing in it is saved; fix: remove it, and Jhon makes it again", queue, err.Error()))
		return
	}
	if lastFlush.Failed != "" {
		d.o.Warn(fmt.Sprintf("Engram queue: Engram did not take the queued records (%q); the next jhon command outside the sandbox tries again", lastFlush.Failed))
	}
	if queued == 0 && refused == 0 {
		d.o.OK(fmt.Sprintf("Engram queue: empty (%q)", queue))
	}
	switch queued {
	case 0:
	case 1:
		d.o.Warn(fmt.Sprintf("Engram queue: 1 state record waits in %q; Jhon saves it outside the sandbox once Engram takes it", queue))
	default:
		d.o.Warn(fmt.Sprintf("Engram queue: %d state records wait in %q; Jhon saves them outside the sandbox once Engram takes them", queued, queue))
	}
	switch refused {
	case 0:
	case 1:
		d.o.Warn(fmt.Sprintf("Engram queue: 1 entry refused in %q, never saved: it was not Jhon's state record; read it, then remove it", filepath.Join(queue, "refused")))
	default:
		d.o.Warn(fmt.Sprintf("Engram queue: %d entries refused in %q, never saved: they were not Jhon's state records; read them, then remove them", refused, filepath.Join(queue, "refused")))
	}
}
