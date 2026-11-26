package integrations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SystemPath is the system's folders an unpacked program finds commands in
// (AskUnpacked), after its own bin and its interpreter's folder.
const SystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// AskUnpacked is `<program> integration --json` of an unpacked release's
// program, before it is installed (T242, odd/tasks/studio-integration-v1.md
// L18 A2): it runs with an environment of HOME (home, also its working
// folder) and PATH (the program's own folder, its interpreter's when its
// first line is `#!/usr/bin/env <name>` or `#!/usr/bin/env -S [options]
// <name>` and the caller finds <name> (interpreterDir), then SystemPath)
// and nothing else, bounded by askTimeout; its answer is read up to
// maxAnswer bytes; when it returns, its process group is killed, and on
// Linux every process it left behind, a setsid one too, and only those
// (reaped). A program that is not executable, or that a noexec mount keeps
// from running, says so.
func AskUnpacked(m Manifest, program, home string) (Answer, error) {
	bin := m.Doctor.Binary
	fi, err := os.Stat(program)
	if err != nil {
		return Answer{}, fmt.Errorf("%s at %s: %v", bin, program, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return Answer{}, fmt.Errorf("%s at %s is not executable", bin, program)
	}
	path := []string{filepath.Dir(program)}
	if d := interpreterDir(program); d != "" {
		path = append(path, d)
	}
	args := []string{"integration", "--json"}
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"HOME=" + home, "PATH=" + strings.Join(append(path, SystemPath), ":")}
	cmd.Dir = home
	ownGroup(cmd)
	cmd.WaitDelay = time.Second
	var out bounded
	cmd.Stdout = &out
	started, err := reaped(ctx, cmd)
	if !started && err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			if mountedNoexec(filepath.Dir(program)) {
				return Answer{}, fmt.Errorf("cannot run %s at %s: its file system is mounted noexec; set TMPDIR to a folder on a file system where programs may run", bin, program)
			}
		}
		return Answer{}, fmt.Errorf("cannot run %s at %s: %w", bin, program, err)
	}
	b, err := answered(ctx, bin, args, err, &out)
	if err != nil {
		return Answer{}, err
	}
	return readAnswer(m, b)
}

// runGrouped runs cmd and kills what is left of its process group once it
// returns; started is whether it ran at all.
func runGrouped(cmd *exec.Cmd) (started bool, err error) {
	if err := cmd.Start(); err != nil {
		return false, err
	}
	err = cmd.Wait()
	killGroup(cmd)
	return true, err
}

// shebangMax is how much of a program's first line the kernel reads
// (BINPRM_BUF_SIZE): interpreterDir reads no more.
const shebangMax = 256

// interpreterDir is the folder, on the caller's PATH, of the interpreter
// program's first line names through env, read as the kernel and env read
// it (T247, odd/tasks/phase1-security.md S4): at most shebangMax bytes, a
// line with no end cut there; the rest after env is one argument, a bare
// name (`#!/usr/bin/env node`) or a split string (`-S`, `-vS`,
// `--split-string=`) whose own options are skipped with their arguments
// (envCommand). "" for any other first line, and for one env would not
// run as written or that jhon does not read (quotes, escapes, variables,
// an option it does not know, a path).
func interpreterDir(program string) string {
	f, err := os.Open(program)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, shebangMax)
	n, _ := io.ReadFull(f, buf)
	line := buf[:n]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	} else if n == shebangMax {
		line = line[:shebangMax-1]
	}
	rest, ok := strings.CutPrefix(string(line), "#!")
	if !ok {
		return ""
	}
	rest = strings.Trim(rest, " \t")
	i := strings.IndexAny(rest, " \t")
	if i < 0 || filepath.Base(rest[:i]) != "env" {
		return ""
	}
	name := envCommand(strings.TrimLeft(rest[i:], " \t"))
	if name == "" || strings.Contains(name, "/") {
		return ""
	}
	p, err := exec.LookPath(name)
	if err != nil || !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Dir(p)
}

// envCommand is the command env runs given arg, its one argument from a
// #! line; "" when it runs none, or jhon does not read it.
func envCommand(arg string) string {
	if strings.ContainsAny(arg, "\r\v\f") {
		// The kernel keeps them; env -S splits on them; a name may hold
		// one: not read (a CRLF line).
		return ""
	}
	if !strings.HasPrefix(arg, "-") {
		if strings.ContainsAny(arg, " \t=") {
			return ""
		}
		return arg
	}
	split, ok := strings.CutPrefix(arg, "--split-string=")
	if !ok {
		if strings.HasPrefix(arg, "--") {
			return ""
		}
		j := 1
		for j < len(arg) && (arg[j] == 'i' || arg[j] == 'v') {
			j++
		}
		if j >= len(arg) || arg[j] != 'S' {
			return ""
		}
		split = arg[j+1:]
	}
	words := strings.Fields(split)
	for i := 0; i < len(words); i++ {
		w := words[i]
		if strings.ContainsAny(w, "'\"\\$") || strings.HasPrefix(w, "#") {
			return ""
		}
		switch {
		case w == "--":
			return envName(words[i+1:])
		case w == "-":
		case strings.HasPrefix(w, "--"):
			name, _, hasValue := strings.Cut(w[2:], "=")
			switch name {
			case "ignore-environment", "debug":
				if hasValue {
					return ""
				}
			case "block-signal", "default-signal", "ignore-signal":
			case "unset", "chdir", "argv0":
				if !hasValue {
					if i++; i >= len(words) {
						return ""
					}
				}
			default:
				return ""
			}
		case strings.HasPrefix(w, "-"):
			for j := 1; j < len(w); j++ {
				switch w[j] {
				case 'i', 'v':
				case 'u', 'C', 'a':
					if j == len(w)-1 {
						if i++; i >= len(words) {
							return ""
						}
					}
					j = len(w)
				default:
					return ""
				}
			}
		default:
			return envName(words[i:])
		}
	}
	return ""
}

// envName is the command of env's words after its options: the first that
// is not NAME=VALUE.
func envName(words []string) string {
	for _, w := range words {
		if strings.ContainsAny(w, "'\"\\$") || strings.HasPrefix(w, "#") {
			return ""
		}
		if !strings.Contains(w, "=") {
			return w
		}
	}
	return ""
}
