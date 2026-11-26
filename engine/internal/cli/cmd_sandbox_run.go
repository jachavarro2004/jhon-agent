package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// sandboxProgram runs a program `jhon sandbox run` starts, bubblewrap or,
// for --profile unsandboxed, the shell itself: path is its file and argv its
// words, argv[0] first, with env. On a terminal it replaces this process,
// as run always did; otherwise it supervises it (sandboxSupervised), and
// a failure ends with failNote when it is not "", any exit but a signal's
// with runNote. refusal is the note on a refused write to a path
// (sandbox.Refusal for the work profile); nil gives sandbox.RefusalNote.
// bound are the descriptors of bubblewrap's binds (sandbox.Open, T248),
// nil for a program with none: they reach the program at the numbers its
// arguments name, and no other descriptor of jhon's does.
func sandboxProgram(path string, argv, env []string, profile, failNote, runNote string, refusal func(string) string, bound *sandbox.Bound) (int, error) {
	if isTerminal(os.Stdout) || isTerminal(os.Stderr) {
		if bound != nil {
			if err := bound.Inherit(); err != nil {
				fmt.Fprintf(os.Stderr, "jhon: exec: %s: cannot pass the sandbox's folders: %v\n", argv[0], err)
				return 126, nil
			}
		}
		return execProgram(path, argv[1:], env)
	}
	if refusal == nil {
		refusal = sandbox.RefusalNote
	}
	var extra []*os.File
	if bound != nil {
		extra = bound.ExtraFiles()
	}
	return sandboxSupervised(path, argv, env, profile, failNote, runNote, refusal, extra)
}

// ownerCredentialHelpers are the owner's credential helpers, the system's
// then the global ones, as git lists them from those files alone (asked in
// / with no repository variable, at most 5 s each): what an unsandboxed git
// line adds back after it resets the repository's (T128g, L1306). The git
// is the first on PATH, which sandbox.RunsOutside found trusted.
func ownerCredentialHelpers() [][2]string {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GIT_DIR" || k == "GIT_WORK_TREE" || k == "GIT_CONFIG_PARAMETERS" || strings.HasPrefix(k, "GIT_CONFIG_COUNT") || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			continue
		}
		env = append(env, kv)
	}
	var out [][2]string
	for _, scope := range []string{"--system", "--global"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, git, "config", scope, "--get-regexp", `^credential\..*helper$`)
		cmd.Dir, cmd.Env = "/", env
		listing, _ := cmd.Output()
		cancel()
		out = append(out, sandbox.Helpers(string(listing))...)
	}
	return out
}

// gitListingLimit bounds the configuration repositoryGitConfigSafe reads
// in one folder: a longer listing runs the line in the sandbox.
const gitListingLimit = 4 << 20

// repositoryGitProblem is why an unsandboxed line of in may not run outside
// the sandbox given each repository it works in (sandbox.GitFolders), ""
// when it may (T249 A2, L39, L40). For each folder, with this process's
// environment without GIT_CONFIG_PARAMETERS (as the line runs) and at most
// 5 s each:
//
//   - its `git config --list --show-scope -z` (at most gitListingLimit
//     bytes) sets only keys on the allowlist, or gate's (the merge gate's
//     entries the line runs with) with their values
//     (sandbox.RepositoryGitConfig);
//   - the git folders `git rev-parse --path-format=absolute --git-dir
//     --git-common-dir` names hold no link and no hard-linked file git
//     writes, and lie in the project (Input.GitFolderProblem). A folder in
//     no repository (gh's, a deploy tool's) has none, when its listing
//     shows no repository scope.
//
// A listing or a rev-parse that fails, times out or is too long is a
// problem too. With no git on PATH the line can run none, and needs no
// check.
func repositoryGitProblem(in sandbox.Input, line string, gate [][2]string) string {
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k != "GIT_CONFIG_PARAMETERS" {
			env = append(env, kv)
		}
	}
	folders := sandbox.GitFolders(line, in.Cwd)
	if folders == nil {
		return "the line cannot be read for the folders its git works in"
	}
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Dir, cmd.Env = "/", env
		out := &limitedBuffer{limit: gitListingLimit}
		cmd.Stdout = out
		err := cmd.Run()
		if err == nil && out.over {
			err = errors.New("its answer is too long")
		}
		return out.String(), err
	}
	for _, dir := range folders {
		listing, err := run("-C", dir, "config", "--list", "--show-scope", "-z")
		if err != nil {
			return "git cannot list the configuration in " + dir + " (" + err.Error() + ")"
		}
		if why := sandbox.RepositoryGitConfig(listing, gate); why != "" {
			return why
		}
		gitDirs, err := run("-C", dir, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
		if err != nil {
			if strings.Contains(listing, "local\x00") || strings.Contains(listing, "worktree\x00") {
				return "git cannot name the git folders in " + dir + " (" + err.Error() + ")"
			}
			continue
		}
		names, why := gitFolderNames(gitDirs)
		if why != "" {
			return why + " in " + dir
		}
		if why := in.GitFolderProblem(names[0], names[1]); why != "" {
			return why
		}
	}
	return ""
}

// gitFolderNames are the git dir and the common dir `git rev-parse
// --path-format=absolute --git-dir --git-common-dir` printed, exactly as git
// uses them (L41): only the final newline is dropped, there are exactly two
// lines, and each is absolute and has no white space around it (a space, a
// tab, U+00A0). git keeps such white space in a path it reads from a file
// (.git/commondir), so a trimmed name would be another folder, a decoy
// beside the one git writes. why is the problem with them, "" when none.
func gitFolderNames(out string) (names []string, why string) {
	body, ok := strings.CutSuffix(out, "\n")
	if !ok {
		return nil, "git named its git folders on no full line"
	}
	names = strings.Split(body, "\n")
	if len(names) != 2 {
		return nil, "git named " + strconv.Itoa(len(names)) + " git folders, not the git dir and the common dir,"
	}
	for _, n := range names {
		if n == "" || !filepath.IsAbs(n) || n != strings.TrimSpace(n) {
			return nil, "git names a git folder " + strconv.Quote(n) + " that is empty, not absolute or has white space around it,"
		}
	}
	return names, ""
}

// limitedBuffer keeps at most limit bytes of what is written to it, and
// whether more came.
type limitedBuffer struct {
	strings.Builder
	limit int
	over  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.over = true
		return 0, errors.New("git config listing too long")
	}
	return b.Builder.Write(p)
}

// pipeGrace is how long, after the program exits, its output pipes may stay
// open (a descendant holding them) before the run ends: pi's shell gives a
// command's quiet pipes the same grace.
const pipeGrace = 100 * time.Millisecond

// forwarded are the signals a supervised run hands to its program.
var forwarded = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// sandboxSupervised runs the program as this process's child with the same
// stdin, its stdout and stderr copied through as they come, and the signals
// this process gets handed to it (T128g, L1303). When it ends:
//
//   - a failure whose output (stdout or stderr) said a path is read-only
//     ends with one note on stderr, refusal's for the path, on a line of
//     its own (G3): for a work run sandbox.Refusal, which says whether the
//     path exists and which setting opens it, or that none does (T239);
//   - a failure, not a signal's, ends with failNote on a line of its own
//     when it is not "": sandbox.DeliveryNote for a work line that holds
//     git over SSH or gh (T181, L1503). bubblewrap reports a shell a
//     signal killed as an exit above 128 (143 for SIGTERM), so such an
//     exit gets no note either; git's own fatal errors exit 128 and keep
//     it;
//   - any exit, a success too, not a signal's, ends with runNote after
//     those, on a line of its own, when it is not "": sandbox.DeployNote
//     for a work line that holds one of sandbox.deploy_tools (T217);
//   - a jhon.sandbox event records the profile, the exit code and the
//     refused path, through telemetry when it is on (G4);
//   - its exit code is the program's, and a program killed by a signal
//     kills this process with the same signal.
//
// extra are the files the program gets besides the standard three
// (exec.Cmd.ExtraFiles): bubblewrap's bind descriptors.
func sandboxSupervised(path string, argv, env []string, profile, failNote, runNote string, refusal func(string) string, extra []*os.File) (int, error) {
	watch := &sandbox.Watch{}
	errOut := &lastByte{w: os.Stderr}
	opening := &firstBytes{}
	outSeen := &firstBytes{}
	cmd := &exec.Cmd{Path: path, Args: argv, Env: env, Stdin: os.Stdin,
		Stdout: io.MultiWriter(os.Stdout, watch, outSeen), Stderr: io.MultiWriter(errOut, watch, opening), WaitDelay: pipeGrace, ExtraFiles: extra}
	// A caller that gave stdout and stderr one file (2>&1) reads one stream
	// in the order it was written: the program gets one pipe for both, as
	// it got the caller's one descriptor before (L1318 c). exec.Cmd gives
	// one pipe when Stdout and Stderr are the same writer.
	if sameFile(os.Stdout, os.Stderr) {
		errOut = &lastByte{w: os.Stdout}
		both := io.MultiWriter(errOut, watch, opening)
		cmd.Stdout, cmd.Stderr = both, both
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, forwarded...)
	defer signal.Stop(signals)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "jhon: exec: %s: cannot execute: %v\n", argv[0], err)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return 127, nil
		}
		return 126, nil
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-signals:
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	_ = cmd.Wait()
	close(done)
	code, killedBy := 1, syscall.Signal(0)
	if st := cmd.ProcessState; st != nil {
		code = st.ExitCode()
		if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			killedBy = ws.Signal()
			code = 128 + int(killedBy)
		}
	}
	refused, said := watch.Refused()
	var notes []string
	// bubblewrap's own refusal of a folder that changed while it mounted it
	// (T248 L4's second verify): its line opens the output, the program
	// never started (nothing on stdout), and bubblewrap exits 1. The line
	// stays; the note says what it means, and no other note follows.
	if profile == sandbox.Work && code == 1 && killedBy == 0 && outSeen.len() == 0 && strings.HasPrefix(opening.String(), bwrapRace) {
		said, failNote, runNote = false, "", ""
		notes = append(notes, bwrapRaceNote)
	}
	if said && code != 0 {
		notes = append(notes, refusal(refused))
	}
	if failNote != "" && code != 0 && code <= 128 && killedBy == 0 {
		notes = append(notes, failNote)
	}
	if runNote != "" && code <= 128 && killedBy == 0 {
		notes = append(notes, runNote)
	}
	if len(notes) > 0 {
		if b := errOut.last(); b != 0 && b != '\n' {
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintln(os.Stderr, strings.Join(notes, "\n"))
	}
	if telemetryOn() {
		attrs := map[string]any{"jhon.sandbox.profile": profile, "process.exit.code": code, "jhon.duration_ms": time.Since(start).Milliseconds()}
		if wd, err := os.Getwd(); err == nil {
			attrs["jhon.cwd"] = wd
		}
		severity := "INFO"
		if code != 0 {
			severity = "WARN"
		}
		if said && code != 0 {
			attrs["jhon.sandbox.refused_write"] = refused
		}
		telemetry.Record(telemetry.Event{Name: "jhon.sandbox", Severity: severity, Attributes: attrs})
	}
	if killedBy != 0 {
		signal.Reset(killedBy)
		_ = syscall.Kill(os.Getpid(), killedBy)
		time.Sleep(100 * time.Millisecond)
	}
	return code, nil
}

// sameFile is whether a and b are one open file (one pipe, one terminal,
// one regular file), as the caller's 2>&1 makes them.
func sameFile(a, b *os.File) bool {
	x, err := a.Stat()
	if err != nil {
		return false
	}
	y, err := b.Stat()
	return err == nil && os.SameFile(x, y)
}

// lastByte is a writer that remembers the last byte written through it.
type lastByte struct {
	mu sync.Mutex
	w  io.Writer
	b  byte
}

func (l *lastByte) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	if n > 0 {
		l.mu.Lock()
		l.b = p[n-1]
		l.mu.Unlock()
	}
	return n, err
}

func (l *lastByte) last() byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b
}

// bwrapRace opens bubblewrap's own refusal of a folder that changed while
// it mounted it, and bwrapRaceNote is what a supervised run adds after it.
const (
	bwrapRace     = "bwrap: Race condition binding dirfd"
	bwrapRaceNote = "jhon sandbox: bubblewrap refused the sandbox because a folder changed while it mounted it, so nothing was run; fix: run the command again"
)

// firstBytes keeps the first bytes written to it, up to 64.
type firstBytes struct {
	mu sync.Mutex
	b  []byte
	n  int
}

func (f *firstBytes) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if room := 64 - len(f.b); room > 0 {
		f.b = append(f.b, p[:min(room, len(p))]...)
	}
	f.n += len(p)
	return len(p), nil
}

func (f *firstBytes) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.b)
}

func (f *firstBytes) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}
