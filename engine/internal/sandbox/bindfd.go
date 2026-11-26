package sandbox

import (
	"errors"
	"os"
	"strconv"
	"syscall"
)

// The check-vs-mount window (T248, odd/tasks/phase1-security.md S3): layout
// judges each area (no link on its way, not nested in another, not the
// home), and bubblewrap mounts it later. A path-based bind follows whatever
// lies at the path by then: a sandboxed run of another project that can
// write a folder on the way (a project under /tmp, a shared
// sandbox.allow_write folder) could swap it for a link in between, and the
// bind would open the folder the link leads to, the home's own, for
// writing. So a run opens each bind (Open) with openat2 from the root,
// RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS (no link on the
// way, the last part included), checks it on the descriptor, and hands
// bubblewrap the descriptor (--bind-fd, --ro-bind-fd): whatever the path
// becomes after that, the mount is the folder that was checked. Read-only
// binds go the same way: one inside a writable area (a PATH folder in a
// sandbox.allow_write folder, Jhon's settings in one) swapped for a link to
// a hidden folder (~/.ssh) would show it, read-only, past its hiding. Only
// the machine's root and /dev stay paths, which no sandboxed command can
// change; the hidden paths (--tmpfs, the null device) are mount points
// bubblewrap makes, with no source to follow.

// Bound is bubblewrap's arguments for one run with each bind opened (Open):
// Args as Args gives them, but each --bind and --ro-bind of an area is a
// --bind-fd or --ro-bind-fd with the descriptor's number and the path next
// to it (--bind-fd 7 /home/u/project), so it stays readable. Files are the
// descriptors, each at its own number, close-on-exec: the run passes them
// to bubblewrap alone (ExtraFiles, Inherit), which closes them before the
// program starts.
type Bound struct {
	Args  []string
	Files []*os.File
}

// Open is Args for in with each bind, writable or read-only, opened with no
// link followed and checked on its descriptor (openBind): a folder is
// still a folder, a file still a file, and a writable one is not the root,
// the home folder or a folder above it. A bind that changed since layout
// judged it refuses the run (changedProblem), and so does a kernel with no
// openat2 or a seccomp filter that refuses it (openat2Problem): a run never
// falls back to a path. The caller closes Bound once bubblewrap started.
func Open(in Input) (*Bound, *Problem) {
	b := &Bound{}
	args, p := build(in, func(m mount) ([]string, *Problem) {
		f, p := openBind(in, m)
		if p != nil {
			return nil, p
		}
		b.Files = append(b.Files, f)
		opt := "--ro-bind-fd"
		if m.kind == write {
			opt = "--bind-fd"
		}
		return []string{opt, strconv.Itoa(int(f.Fd())), m.path}, nil
	})
	if p != nil {
		b.Close()
		return nil, p
	}
	b.Args = args
	return b, nil
}

// ExtraFiles are b's files as exec.Cmd.ExtraFiles takes them: each at the
// number Args names (entry i is descriptor 3+i), nil between them, which
// the child closes.
func (b *Bound) ExtraFiles() []*os.File {
	var out []*os.File
	for _, f := range b.Files {
		n := int(f.Fd()) - 3
		for len(out) <= n {
			out = append(out, nil)
		}
		out[n] = f
	}
	return out
}

// Close closes b's files; a nil b has none.
func (b *Bound) Close() {
	if b == nil {
		return
	}
	for _, f := range b.Files {
		f.Close()
	}
	b.Files = nil
}

// Problems of Open.
var (
	// errNoOpenat2 is a kernel with no openat2 (ENOSYS), errOpenat2Refused
	// one whose seccomp filter refuses it (EPERM), as the Engram queue
	// names them (T227).
	errNoOpenat2      = errors.New("this kernel has no openat2 (Linux 5.6 or newer), which the sandbox needs to bind its folders with no link followed")
	errOpenat2Refused = errors.New("openat2 is refused here (operation not permitted: a seccomp filter, as a container's, blocks it), which the sandbox needs to bind its folders with no link followed")
)

// openat2Problem is the run failing closed for err, an openat2 that is not
// there or refused; nil for any other error.
func openat2Problem(err error) *Problem {
	switch {
	case errors.Is(err, syscall.ENOSYS):
		return &Problem{Reason: errNoOpenat2.Error(), Fix: "a Linux kernel 5.6 or newer"}
	case errors.Is(err, syscall.EPERM):
		return &Problem{Reason: errOpenat2Refused.Error(), Fix: "allow openat2 in the seccomp filter jhon runs under (a container's profile), or run jhon outside it"}
	}
	return nil
}

// changedProblem is the run refused for m, a bind that changed after
// layout judged it: why says how (it is or passes through a link now, it is
// gone, ...).
func changedProblem(m mount, why string) *Problem {
	what := "the file "
	if m.dir {
		what = "the folder "
	}
	how := ", which the sandbox keeps read-only, "
	if m.kind == write {
		how = ", which the sandbox opens for writing, "
	}
	return &Problem{
		Reason: what + m.path + how + "changed after Jhon checked it: " + why,
		Fix:    "run the command again; if it is refused again, a command may be replacing " + m.path + ": find it and stop it",
	}
}
